package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct — Wails 桌面 app 后端.
//
// 桌面 app 通过 Cloudflare Tunnel 调用 novel2all-go 后端:
//   - 后端地址: https://auth.zxc.im (Phase 0 部署, 公网可达)
//   - JWT 存储: 本地 SQLite (Phase 2 加入, Phase 1 暂用内存)
//   - LLM keys: Phase 2 加密存储, Phase 1 暂用 placeholder
//
// 所有 API 方法都通过 wails bridge 暴露给前端 (frontend/src/).
type App struct {
	ctx    context.Context
	client *http.Client

	// backendURL 后端基地址 (Cloudflare Tunnel 域名).
	// Phase 1 硬编码; Phase 2 改成设置页面可配 (env 或 DB).
	backendURL string

	// auth 状态.
	mu          sync.Mutex
	accessToken string
	refreshTok  string
	expiresAt   time.Time
	userInfo    *User
}

// User 登录用户信息 (Phase 1 简化版).
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Email    string `json:"email,omitempty"`
}

// Project 后端 projects 集合的镜像.
type Project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	Genre       string `json:"genre,omitempty"`
	OwnerID     int64  `json:"owner_id"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// Chapter 后端 chapters 集合的镜像.
type Chapter struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Number    int    `json:"number"`
	Title     string `json:"title,omitempty"`
	Filename  string `json:"filename"`
	CharCount int    `json:"char_count,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// APIError 后端错误响应格式.
type APIError struct {
	Error string `json:"error"`
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		backendURL: "https://api.zxc.im", // Cloudflare Tunnel 域名 (Phase 0)
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 加载持久化 token (从用户配置目录)
	if tok := a.loadTokenFromDisk(); tok != "" {
		a.mu.Lock()
		a.accessToken = tok
		a.mu.Unlock()
		runtime.LogInfo(ctx, fmt.Sprintf("loaded cached access token (%.20s...)", tok))
	}

	// 启动后台 token 自动续期
	go a.tokenAutoRefresh(ctx)

	runtime.LogInfo(ctx, fmt.Sprintf("novel2all-desktop started; backend=%s", a.backendURL))
}

// onShutdown 是 wails OnShutdown hook. 保存 token 到磁盘.
func (a *App) onShutdown(ctx context.Context) {
	a.mu.Lock()
	tok := a.accessToken
	a.mu.Unlock()
	if tok != "" {
		_ = a.saveTokenToDisk(tok)
	}
}

// ---------------------------------------------------------------------------
// Backend health
// ---------------------------------------------------------------------------

// BackendURL 返后端地址 (前端显示).
func (a *App) BackendURL() string {
	return a.backendURL
}

// HealthCheck 调后端 /health. 返 (ok, message).
//
// 前端用此检查 Wails app → 后端 → Tunnel 网络全链路.
func (a *App) HealthCheck() (bool, string) {
	resp, err := a.client.Get(a.backendURL + "/health")
	if err != nil {
		return false, "backend unreachable: " + err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("backend returned HTTP %d", resp.StatusCode)
	}

	var body struct {
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
		Uptime    string `json:"uptime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, "decode failed: " + err.Error()
	}
	if body.Status != "ok" {
		return false, "backend status != ok"
	}
	return true, body.Timestamp
}

// ---------------------------------------------------------------------------
// Auth (Phase 1 placeholder)
// ---------------------------------------------------------------------------

// CurrentUser 返当前登录用户 (无登录则 nil).
func (a *App) CurrentUser() *User {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.userInfo
}

// IsLoggedIn 简单判断是否有有效 token.
func (a *App) IsLoggedIn() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.accessToken != "" && time.Now().Before(a.expiresAt)
}

// Login 调后端 /api/auth/login. Phase 1 仅 mock (真实端点 Phase 2).
//
// 当前 Phase 1: 任何 username/password 都返 mock user. Phase 2 替换为真实调用.
func (a *App) Login(username, password string) (*User, error) {
	if username == "" || password == "" {
		return nil, fmt.Errorf("username/password 不能为空")
	}

	// Phase 1: 直接调后端 /version 验证网络 (mock 成功登录).
	// Phase 2: 替换为 POST /api/auth/login + JWT.
	resp, err := a.client.Get(a.backendURL + "/version")
	if err != nil {
		return nil, fmt.Errorf("后端不可达: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("后端返回 HTTP %d", resp.StatusCode)
	}

	// 模拟登录成功 (Phase 2 替换)
	user := &User{
		ID:       1,
		Username: username,
		Role:     "admin",
		Email:    username + "@novel2all.local",
	}

	a.mu.Lock()
	a.accessToken = "phase1-mock-token-" + username
	a.refreshTok = ""
	a.expiresAt = time.Now().Add(24 * time.Hour)
	a.userInfo = user
	a.mu.Unlock()

	return user, nil
}

// Logout 清 token + 磁盘缓存.
func (a *App) Logout() error {
	a.mu.Lock()
	a.accessToken = ""
	a.refreshTok = ""
	a.expiresAt = time.Time{}
	a.userInfo = nil
	a.mu.Unlock()

	// 删磁盘缓存
	if path, err := a.tokenPath(); err == nil {
		_ = os.Remove(path)
	}
	return nil
}

// tokenAutoRefresh 后台 goroutine: token 过期前 5 分钟自动续期.
func (a *App) tokenAutoRefresh(ctx context.Context) {
	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.mu.Lock()
			expires := a.expiresAt
			a.mu.Unlock()
			if expires.IsZero() {
				continue
			}
			if time.Until(expires) < 5*time.Minute {
				// Phase 2: 调 /api/auth/refresh
				// Phase 1: 无操作 (mock token)
				runtime.LogInfo(ctx, "token refresh skipped (Phase 1 mock)")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Projects / Chapters (Phase 1: 直接代理后端 REST API)
// ---------------------------------------------------------------------------

// doRequest 带 auth 的 HTTP 请求 (Phase 1 不传 token, 后端 public endpoints OK).
func (a *App) doRequest(method, path string, body any) (*http.Response, error) {
	url := a.backendURL + path

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	a.mu.Lock()
	tok := a.accessToken
	a.mu.Unlock()
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	return resp, nil
}

// ListProjects 调后端 GET /api/projects. admin sees all, user sees own.
//
// Phase 1: admin 角色返全部 (假设 admin 登录); 其他返空 (Phase 2 接入 owner filter 后正常).
func (a *App) ListProjects() ([]Project, error) {
	resp, err := a.doRequest(http.MethodGet, "/api/projects", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Projects []Project `json:"projects"`
		Count    int       `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out.Projects, nil
}

// GetProject 调后端 GET /api/projects/{id}.
func (a *App) GetProject(id int64) (*Project, error) {
	resp, err := a.doRequest(http.MethodGet, fmt.Sprintf("/api/projects/%d", id), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var p Project
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &p, nil
}

// ListChapters 调后端 GET /api/projects/{id}/chapters.
func (a *App) ListChapters(projectID int64) ([]Chapter, error) {
	resp, err := a.doRequest(http.MethodGet, fmt.Sprintf("/api/projects/%d/chapters", projectID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Chapters []Chapter `json:"chapters"`
		Count    int       `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out.Chapters, nil
}

// ---------------------------------------------------------------------------
// 磁盘 token 缓存 (用户配置目录)
// ---------------------------------------------------------------------------

// tokenPath 返 token 缓存文件路径.
//
// Windows: %APPDATA%\novel2all-desktop\token
// 其他:    ~/.config/novel2all-desktop/token
func (a *App) tokenPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "novel2all-desktop")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(appDir, "token"), nil
}

func (a *App) loadTokenFromDisk() string {
	path, err := a.tokenPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func (a *App) saveTokenToDisk(tok string) error {
	path, err := a.tokenPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(tok), 0o600)
}
