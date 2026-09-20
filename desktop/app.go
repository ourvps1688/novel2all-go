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
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"desktop-novel2all/internal/secrets"
	"desktop-novel2all/internal/systray"
	"desktop-novel2all/internal/update"
)

// App struct — Wails 桌面 app 后端.
//
// 桌面 app 通过 Cloudflare Tunnel 调用 novel2all-go 后端:
//   - 后端地址: https://api.zxc.im (Phase 0 部署, 公网可达)
//   - JWT 存储: 本地 SQLite (Phase 2 加入, Phase 1 暂用内存)
//   - LLM keys: Module B (2026-09-20) AES-256-GCM 加密存 %APPDATA%\novel2all-desktop\llm_keys.enc
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

	// trayMenuRef 系统托盘引用 (Phase 1.4).
	// nil = systray 未启用 (开发模式).
	trayMenuRef *systray.Menu

	// llmSecrets 加密 LLM API key 存储 (Module B).
	// nil = 初始化失败 (设置页面 LLM 区会显示错误).
	llmSecrets *secrets.Store
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
//
// Sprint V1.0.1 P2 修复: JSON tag 必须对齐后端 (api.ChapterInfo chapter + char_count),
// 之前用 json:"number" → 后端返 "chapter" 字段, 反序列化默认 0 → 桌面显示"第0章",
// 编辑/删除按钮调 GetChapterContent(id, c.number=undefined) → Go int=0 → server 400 invalid.
type Chapter struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Chapter   int    `json:"chapter"` // 后端字段名 chapter (api.ChapterInfo.Chapter)
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

// Character 人物 (Module D - 知识管理).
//
// 后端 internal/api/characters.go Character struct 镜像:
//   - json tags 必须与后端一致 (snake_case)
//   - ID 由后端 nextID 分配, 创建时不传
type Character struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`                     // protagonist/antagonist/supporting
	Description  string   `json:"description,omitempty"`
	FirstChapter int      `json:"first_chapter,omitempty"`
	LastChapter  int      `json:"last_chapter,omitempty"`
	Traits       []string `json:"traits,omitempty"`
}

// Relationship 人物关系 (Module D).
//
// character_a + character_b 用名字引用 (不是 ID), 简单 + 用户友好.
type Relationship struct {
	ID          int64  `json:"id"`
	CharacterA  string `json:"character_a"` // 人物 A 名字
	CharacterB  string `json:"character_b"` // 人物 B 名字
	Type        string `json:"type"`        // friend/foe/family/romantic/rival/...
	Description string `json:"description,omitempty"`
}

// Foreshadow 伏笔 (Module D).
//
// planted_chapter 必填, payoff_chapter 可空 (伏笔未揭示).
// status: active (default) / resolved / abandoned.
type Foreshadow struct {
	ID             int64  `json:"id"`
	Title          string `json:"title"`
	PlantedChapter int    `json:"planted_chapter"`
	PayoffChapter  int    `json:"payoff_chapter,omitempty"`
	Status         string `json:"status"`
	Description    string `json:"description,omitempty"`
}

// OutlineItem 章节大纲 (Module E - 2026-09-20).
//
// 后端 internal/api/outline.go OutlineItem struct 镜像.
// chapter: 章节号 (1-based, 在项目内唯一).
// key_events: 关键事件列表.
// status: planned (default) / in_progress / done.
// characters/foreshadows: 关联名 (与 Module D characters/foreshadows 弱类型关联).
type OutlineItem struct {
	ID          int64    `json:"id"`
	ProjectID   int64    `json:"project_id"`
	Chapter     int      `json:"chapter"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary,omitempty"`
	KeyEvents   []string `json:"key_events,omitempty"`
	Status      string   `json:"status"`
	Notes       string   `json:"notes,omitempty"`
	Characters  []string `json:"characters,omitempty"`
	Foreshadows []string `json:"foreshadows,omitempty"`
}

// SkillSummary 技能概要 (Module H - 2026-09-20).
//
// 后端 internal/api/skills.go SkillSummary struct 镜像.
// 后端 GET /api/skills 返回 13 个 SKILL.md 的 name + description 列表.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillExecuteResult 同步执行 skill 的结果 (Module H).
//
// 后端 ExecuteResult 字段镜像. 桌面只暴露同步版本 (execute-sync),
// 流式 (SSE) 后续 Sprint 优化再加入 (需后端持续推送 + Wails events emit).
type SkillExecuteResult struct {
	Content   string `json:"content"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`
}

// ActionRequest chapter action LLM 调用的请求体 (Module C).
type ActionRequest struct {
	ProjectID   int64  `json:"project_id"`
	Instruction string `json:"instruction,omitempty"`
	Position    int    `json:"position,omitempty"`
}

// ActionResponse chapter action LLM 调用的响应 (Module C).
//
// 4 个 action (expand/rewrite/insert/rollback) 都返这个.
// review 单独返 ReviewResult (因字段差异大).
type ActionResponse struct {
	Chapter       int               `json:"chapter"`
	Action        string            `json:"action"`
	OutputPath    string            `json:"output_path"`
	BackupPath    string            `json:"backup_path,omitempty"`
	CharsBefore   int               `json:"chars_before,omitempty"`
	CharsAfter    int               `json:"chars_after,omitempty"`
	AppendedChars int               `json:"appended_chars,omitempty"`
	ElapsedMS     int64             `json:"elapsed_ms"`
	Message       string            `json:"message,omitempty"`
	Issues        []PostCheckIssue  `json:"issues,omitempty"`
}

// ReviewResult review action 专属响应 (Module C).
//
// review 不改文件, 只评估, 所以字段集不同.
type ReviewResult struct {
	Chapter        int          `json:"chapter"`
	CriticalIssues []ReviewItem  `json:"critical_issues"`
	MajorIssues    []ReviewItem  `json:"major_issues"`
	MinorIssues    []ReviewItem  `json:"minor_issues"`
	QualityScore   float64      `json:"quality_score"`
	OverallVerdict string       `json:"overall_verdict"`
	ContentChars   int          `json:"content_chars"`
	ElapsedMS      int64        `json:"elapsed_ms"`
}

// PostCheckIssue verifier post-check 报告的单条问题 (Module C).
type PostCheckIssue struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

// ReviewItem review 单条反馈 (Module C).
type ReviewItem struct {
	Location string `json:"location,omitempty"`
	Category string `json:"category,omitempty"`
	Note     string `json:"note"`
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		client: &http.Client{
			// Module C.5 (2026-09-20) 修复 LLM 超时:
			// - 120s 太短: 用户实测 "扩写" 在首字节到达前 abort (后端 handler 在 LLM
			//   调用前没 write response headers, client 一直等 headers)
			// - 600s = 10 分钟上限: 覆盖 LLM 冷启动 (60-180s) + 稳态调用 (5-30s) + 网络抖动
			// - 后端改进中期: expand/rewrite handler 立即 WriteHeader(200) + Flush, 让
			//   client 拿到 headers 后即使 LLM 阻塞也不 abort
			// - UI 改进: elapsed timer 显示等待秒数 + 30s 后提示
			Timeout: 600 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// POST 后端返 301 redirect (→ /api/projects/) 是路由注册 bug.
				// 不跟随 redirect, 直接读 POST 响应 body.
				return http.ErrUseLastResponse
			},
		},
		backendURL: "https://api.zxc.im", // Cloudflare Tunnel 域名 (Phase 0)
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	AppCtx = ctx
	a.ctx = ctx

	// 加载持久化 token (从用户配置目录)
	if tok := a.loadTokenFromDisk(); tok != "" {
		a.mu.Lock()
		a.accessToken = tok
		a.mu.Unlock()
		runtime.LogInfo(ctx, fmt.Sprintf("loaded cached access token (%.20s...)", tok))
	}

	// Module B: 初始化加密 LLM key 存储
	// 失败不阻塞 startup (UI 会显示"LLM keys 不可用")
	if path, err := a.llmKeysPath(); err == nil {
		if store, err := secrets.New(path); err == nil {
			a.llmSecrets = store
			runtime.LogInfo(ctx, fmt.Sprintf("llm_keys store ready: %s", path))
		} else {
			runtime.LogErrorf(ctx, "llm_keys store init failed: %v", err)
		}
	} else {
		runtime.LogErrorf(ctx, "llm_keys path resolution failed: %v", err)
	}

	// 启动后台 token 自动续期
	go a.tokenAutoRefresh(ctx)

	runtime.LogInfo(ctx, fmt.Sprintf("novel2all-desktop started; backend=%s", a.backendURL))

	// Phase 1.4: 后台异步检测后端健康 + 更新托盘状态.
	// 不阻塞 startup, 失败也无所谓 (前端还有 HealthCheck 兜底).
	if a.trayMenuRef != nil {
		go func() {
			time.Sleep(500 * time.Millisecond)
			ok, _ := a.HealthCheck()
			if ok {
				a.trayMenuRef.SetStatusText("状态: 已连接")
			} else {
				a.trayMenuRef.SetStatusText("状态: 后端离线")
			}
		}()
	}
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

// Login 调后端 /api/auth/login-jwt (Phase 2 桌面 app 真实登录).
//
// 流程:
//  1. POST {username, password}
//  2. 后端 bcrypt 验证 + 创建 session + 写审计
//  3. 返 {access_token, expires_in, token_type, username, role}
//  4. 桌面 app 存 token 到 %APPDATA%\novel2all-desktop\token (doRequest 自动加 Bearer header)
func (a *App) Login(username, password string) (*User, error) {
	if username == "" || password == "" {
		return nil, fmt.Errorf("username/password 不能为空")
	}

	// Phase 2: POST /api/auth/login-jwt (Bearer token JSON 版, 不写 cookie)
	body := map[string]string{"username": username, "password": password}
	resp, err := a.doRequest(http.MethodPost, "/api/auth/login-jwt", body)
	if err != nil {
		return nil, fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("登录失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var loginResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
		Username    string `json:"username"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("解析登录响应失败: %w", err)
	}
	if loginResp.AccessToken == "" {
		return nil, fmt.Errorf("后端未返回 access_token")
	}

	user := &User{
		ID:       0, // Phase 2.1 不查 /me, ID 留 0; Phase 2.2 加 /me 查询
		Username: loginResp.Username,
		Role:     loginResp.Role,
		Email:    "", // Phase 2 暂不查 email
	}

	a.mu.Lock()
	a.accessToken = loginResp.AccessToken
	a.refreshTok = ""
	a.expiresAt = time.Now().Add(time.Duration(loginResp.ExpiresIn) * time.Second)
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

	// Module B.2 (2026-09-20): 自动注入 user LLM key header.
	// 后端 middleware (LLMAPIKeyMiddleware) 读这些 header 透传给 LLM provider,
	// 覆盖 systemd 环境变量里的 admin key. 没配置 user key 的 provider 不加 header.
	if a.llmSecrets != nil {
		for provider, headerName := range userKeyHeaderMap {
			if key, err := a.llmSecrets.Get(provider); err == nil && key != "" {
				req.Header.Set(headerName, key)
			}
		}
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	return resp, nil
}

// userKeyHeaderMap: desktop provider 名 → 后端 X-LLM-Key-* header 名.
//
// 后端常量定义在 internal/api/middleware_llmkey.go. 这里保持字面值以避免
// 跨 module 依赖 (desktop 不能 import backend internal 包).
//
// ⚠️ 改名时双方必须同步 (改名后 grep 整个项目同步).
var userKeyHeaderMap = map[string]string{
	"dashscope": "X-LLM-Key-DashScope",
	"deepseek":  "X-LLM-Key-DeepSeek",
	"minimax":   "X-LLM-Key-Minimax",
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

// ---------------------------------------------------------------------------
// Module F: Project CRUD
// ---------------------------------------------------------------------------

// ProjectInput 创建/更新 project 的请求体 (Phase 1: 简单结构, Phase 2 扩展 genre/slug 等).
type ProjectInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	Description string `json:"description,omitempty"`
	Genre       string `json:"genre,omitempty"`
}

// CreateProject 调后端 POST /api/projects/.
//
// 后端 POST 返 301 → /api/projects/ 是路由注册时遗留的小 bug (Phase 1).
// 桌面 app 直接 POST /api/projects/ 命中正确路由 + 接收 JSON.
func (a *App) CreateProject(input ProjectInput) (*Project, error) {
	resp, err := a.doRequest(http.MethodPost, "/api/projects/", input)
	if err != nil {
		return nil, fmt.Errorf("创建项目失败: %w", err)
	}
	defer resp.Body.Close()

	// 接受 200 OK / 201 Created / 302 Found (redirect 后的 GET).
	// 401/403/500 等明确失败.
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusFound:
		// 201/200: 解析 JSON. 302: 通常 redirect 后 GET 也成功.
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("创建失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// POST 通常返创建的对象 (201). 若 302 没 body, fallback 到 GET 列表.
	var p Project
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		// 302 + 空 body → 重查列表
		projects, listErr := a.ListProjects()
		if listErr != nil || len(projects) == 0 {
			return nil, fmt.Errorf("decode: %w (fallback list 也失败: %v)", err, listErr)
		}
		// 找到刚创建的 (name match)
		for _, proj := range projects {
			if proj.Name == input.Name {
				return &proj, nil
			}
		}
		// fallback: 取最新
		return &projects[len(projects)-1], nil
	}
	return &p, nil
}

// UpdateProject 调后端 PUT /api/projects/{id}.
func (a *App) UpdateProject(id int64, input ProjectInput) (*Project, error) {
	resp, err := a.doRequest(http.MethodPut, fmt.Sprintf("/api/projects/%d", id), input)
	if err != nil {
		return nil, fmt.Errorf("更新项目失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusFound:
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("更新失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var p Project
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &p, nil
}

// DeleteProject 调后端 DELETE /api/projects/{id}.
//
// 删除后项目消失 (Phase 1: 后端 soft delete 未实现 → 硬删除).
func (a *App) DeleteProject(id int64) error {
	resp, err := a.doRequest(http.MethodDelete, fmt.Sprintf("/api/projects/%d", id), nil)
	if err != nil {
		return fmt.Errorf("删除项目失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusFound, http.StatusSeeOther:
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("删除失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// ListChapters 调后端 GET /api/chapters?project_id=N.
func (a *App) ListChapters(projectID int64) ([]Chapter, error) {
	resp, err := a.doRequest(http.MethodGet, fmt.Sprintf("/api/chapters?project_id=%d", projectID), nil)
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
// Module A: Chapter CRUD (后端 chapter endpoint 代理)
// ---------------------------------------------------------------------------

// ChapterInput 创建/更新章节的请求体 (Phase 1: 简单结构).
//
// Sprint V1.0.1 P2 修复: 字段名 Chapter (json:"chapter") 与后端对齐 (api.ChapterInfo.Chapter),
// 之前用 json:"number" → 不一致 (后端从 URL path 拿 N, body 里 number 字段无意义,
// 但 Wails 生成的 TS binding 用 Go 字段名 → 前端传 number → Go 收到 0 → URL /api/chapter/0).
type ChapterInput struct {
	ProjectID int64  `json:"project_id"`
	Chapter   int    `json:"chapter"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content,omitempty"`
}

// ChapterContent 后端 chapter content endpoint 响应.
type ChapterContent struct {
	Chapter   int    `json:"chapter"`
	Filename  string `json:"filename"`
	Content   string `json:"content"`
	CharCount int    `json:"char_count"`
	FirstLine string `json:"first_line"`
}

// CreateChapter 调后端 POST /api/chapter/{N} 创建新章节.
//
// chapter 必须 > 0. 同一 chapter 已存在返 409 Conflict.
func (a *App) CreateChapter(input ChapterInput) (*Chapter, error) {
	if input.Chapter <= 0 {
		return nil, fmt.Errorf("chapter number 必须 > 0")
	}
	if input.ProjectID <= 0 {
		return nil, fmt.Errorf("project_id 必须 > 0")
	}
	resp, err := a.doRequest(http.MethodPost, fmt.Sprintf("/api/chapter/%d", input.Chapter), input)
	if err != nil {
		return nil, fmt.Errorf("创建章节失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK, http.StatusFound:
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("创建失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var respBody struct {
		Chapter   int    `json:"chapter"`
		Title     string `json:"title"`
		CharCount int    `json:"char_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		// 302 + 空 body fallback: 重查 list 匹配 chapter
		chapters, listErr := a.ListChapters(input.ProjectID)
		if listErr != nil {
			return nil, fmt.Errorf("decode: %w (fallback list 失败: %v)", err, listErr)
		}
		for _, c := range chapters {
			if c.Chapter == input.Chapter {
				return &c, nil
			}
		}
		return nil, fmt.Errorf("decode: %w (fallback list 也找不到 chapter=%d)", err, input.Chapter)
	}
	// 构造 Chapter 响应
	return &Chapter{
		ProjectID: input.ProjectID,
		Chapter:   respBody.Chapter,
		Title:     respBody.Title,
		CharCount: respBody.CharCount,
		Filename:  fmt.Sprintf("第%03d章.md", respBody.Chapter),
	}, nil
}

// GetChapterContent 调后端 GET /api/chapter/{N}/content?project_id=M 取完整 markdown.
func (a *App) GetChapterContent(projectID int64, number int) (*ChapterContent, error) {
	resp, err := a.doRequest(http.MethodGet, fmt.Sprintf("/api/chapter/%d/content?project_id=%d", number, projectID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var content ChapterContent
	if err := json.NewDecoder(resp.Body).Decode(&content); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &content, nil
}

// UpdateChapter 调后端 POST /api/chapter/{N}/save 更新章节内容 + title.
func (a *App) UpdateChapter(input ChapterInput) (*Chapter, error) {
	if input.Chapter <= 0 {
		return nil, fmt.Errorf("chapter number 必须 > 0")
	}
	if input.ProjectID <= 0 {
		return nil, fmt.Errorf("project_id 必须 > 0")
	}
	body := map[string]any{
		"project_id": input.ProjectID,
		"title":      input.Title,
		"content":    input.Content,
	}
	resp, err := a.doRequest(http.MethodPost, fmt.Sprintf("/api/chapter/%d/save", input.Chapter), body)
	if err != nil {
		return nil, fmt.Errorf("更新章节失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("更新失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}
	// save 返回 {"chapter":N, "char_count":N, "output_path":"..."}
	var respBody struct {
		Chapter   int `json:"chapter"`
		CharCount int `json:"char_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &Chapter{
		ProjectID: input.ProjectID,
		Chapter:   respBody.Chapter,
		Title:     input.Title,
		CharCount: respBody.CharCount,
		Filename:  fmt.Sprintf("第%03d章.md", respBody.Chapter),
	}, nil
}

// DeleteChapter 调后端 DELETE /api/chapter/{N}?project_id=M 删除章节文件 + metadata.
func (a *App) DeleteChapter(projectID int64, number int) error {
	resp, err := a.doRequest(http.MethodDelete, fmt.Sprintf("/api/chapter/%d?project_id=%d", number, projectID), nil)
	if err != nil {
		return fmt.Errorf("删除章节失败: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusFound, http.StatusSeeOther:
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("删除失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
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

// ---------------------------------------------------------------------------
// Auto-update (Phase 3)
// ---------------------------------------------------------------------------

// CheckForUpdate 调 GitHub Releases API 检查更新.
//
// 返 update.Info (前端设置页显示).
func (a *App) CheckForUpdate() (*update.UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return update.CheckForUpdates(ctx)
}

// CurrentVersion 返 desktop 当前版本 (前端显示).
func (a *App) CurrentVersion() string {
	return update.CurrentVersion
}

// DownloadUpdate 下载最新版 .exe 到 %TEMP%.
//
// 返 (path, info, error). 路径给前端传给 ApplyUpdate.
// DownloadProgress callback 通过 runtime.EventsEmit 推到前端 (SSE).
func (a *App) DownloadUpdate() (string, *update.UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	info, err := a.CheckForUpdate()
	if err != nil {
		return "", nil, err
	}
	if info == nil || !info.Available {
		return "", info, fmt.Errorf("no update available")
	}

	progress := func(downloaded, total int64) {
		runtime.EventsEmit(a.ctx, "update:progress", map[string]any{
			"downloaded": downloaded,
			"total":      total,
			"percent":    int(downloaded * 100 / total),
		})
	}

	path, err := update.DownloadLatest(ctx, info, progress)
	if err != nil {
		return "", info, err
	}
	return path, info, nil
}

// ApplyUpdate 启动 NSIS installer 应用更新.
//
// installerPath 是 DownloadUpdate 返的路径. 调用后当前进程需退出
// (NSIS 升级机制接管), Phase 4 改用更可靠的退出逻辑.
func (a *App) ApplyUpdate(installerPath string) error {
	if err := update.ApplyUpdate(installerPath); err != nil {
		return err
	}
	// 启动 installer 成功, 退出当前 app 让 NSIS 替换 binary
	runtime.Quit(a.ctx)
	return nil
}

// ---------------------------------------------------------------------------
// Module B: LLM API key 加密存储 (wails-bound methods)
// ---------------------------------------------------------------------------

// llmKeysPath 返 LLM key 加密文件的完整路径.
//
// Windows: %APPDATA%\novel2all-desktop\llm_keys.enc
// 其他:    ~/.config/novel2all-desktop/llm_keys.enc
//
// 与 tokenPath() 同结构 (Phase 1 一致性).
func (a *App) llmKeysPath() (string, error) {
	return secrets.DefaultPath()
}

// supportedProviders 是 UI 显示的 provider 列表 (硬编码, 跟后端 model_mapping 一致).
//
// 暂时不在文件里读取, 因为 model_mapping 是 server 端配置, client 这边 hard-code 更稳.
// 后续可改成 /api/llm/providers 端点返回.
var supportedProviders = []string{"dashscope", "deepseek", "minimax"}

// SupportedProviders 返 UI 显示的 provider 列表 (前端 hard-code 不安全, 后端权威).
func (a *App) SupportedProviders() []string {
	out := make([]string, len(supportedProviders))
	copy(out, supportedProviders)
	return out
}

// GetLLMKeys 返已配置的 provider 名称列表 (按字母序).
//
// 只返名字, 不返 key 内容 (防御性: 即使调用方有 Wails bridge access, 也不暴露明文).
// 前端用此决定每个 provider 输入框的"已配置 ✓" badge.
func (a *App) GetLLMKeys() ([]string, error) {
	if a.llmSecrets == nil {
		return nil, fmt.Errorf("LLM key 存储未初始化")
	}
	names, err := a.llmSecrets.List()
	if err != nil {
		return nil, fmt.Errorf("读取 LLM keys 失败: %w", err)
	}
	return names, nil
}

// SetLLMKey 设置单个 provider 的 API key. 空 key 等同于删除.
//
// 立即加密落盘. 失败返 error (前端显示).
func (a *App) SetLLMKey(provider, key string) error {
	if a.llmSecrets == nil {
		return fmt.Errorf("LLM key 存储未初始化")
	}
	provider = strings.TrimSpace(provider)
	key = strings.TrimSpace(key)
	if provider == "" {
		return fmt.Errorf("provider 不能为空")
	}
	// 可选: 限制 provider 在 supportedProviders (防 typo 存到无效 key)
	valid := false
	for _, p := range supportedProviders {
		if p == provider {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("不支持的 provider: %s (支持: %v)", provider, supportedProviders)
	}
	if err := a.llmSecrets.Set(provider, key); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	runtime.LogInfo(a.ctx, fmt.Sprintf("llm key set: %s (len=%d)", provider, len(key)))
	return nil
}

// ClearLLMKeys 删除所有 LLM keys (重置 / 隐私清理).
//
// 删除整个加密文件, 不只是清空 map. 下次 Set 重新生成.
func (a *App) ClearLLMKeys() error {
	if a.llmSecrets == nil {
		return fmt.Errorf("LLM key 存储未初始化")
	}
	if err := a.llmSecrets.Clear(); err != nil {
		return fmt.Errorf("清除失败: %w", err)
	}
	runtime.LogInfo(a.ctx, "llm keys cleared")
	return nil
}

// HasLLMKey 检查指定 provider 是否已配置 key (UI 用).
func (a *App) HasLLMKey(provider string) (bool, error) {
	if a.llmSecrets == nil {
		return false, nil // 静默 false, UI 当作"未配置"
	}
	return a.llmSecrets.Has(provider)
}

// ---------------------------------------------------------------------------
// Module C: 章节 action (LLM 调用: expand/rewrite/review/insert/rollback)
// ---------------------------------------------------------------------------

// ChapterAction 5 个 LLM action 的统一调用逻辑.
//
// 后端 dispatch:
//   POST /api/chapter/{N}/{action}/  body={project_id, instruction, position}
//
// 返回: 4 个 action (expand/rewrite/insert/rollback) 返 ActionResponse
//
//	review 返 ReviewResult (字段差异大, 由调用方分别解析)
//
// 错误处理:
//   - HTTP 非 200 → 解析 error body 抛 wrapped error
//   - LLM 调用失败/超时 → 后端已返回 500, 桌面原样返回 error
func (a *App) callChapterAction(projectID int64, chapter int, action, instruction string, position int, reviewMode bool) (string, error) {
	if projectID <= 0 {
		return "", fmt.Errorf("project_id 必须 > 0")
	}
	if chapter <= 0 {
		return "", fmt.Errorf("chapter 必须 > 0")
	}
	req := ActionRequest{
		ProjectID:   projectID,
		Instruction: instruction,
		Position:    position,
	}
	// URL path: /api/chapter/{N}/{action}/  (后端 router 同时注册有/无 trailing slash)
	url := fmt.Sprintf("/api/chapter/%d/%s", chapter, action)
	resp, err := a.doRequest(http.MethodPost, url, req)
	if err != nil {
		return "", fmt.Errorf("%s 请求失败: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%s 失败 (HTTP %d): %s", action, resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读响应失败: %w", err)
	}
	return string(body), nil
}

// ExpandChapter 调 LLM 扩写章节 (追加内容).
//
// 后端: POST /api/chapter/{N}/expand/  +  body={project_id, instruction}
// 返回: ActionResponse (含 chars_before/chars_after/appended_chars + elapsed_ms)
func (a *App) ExpandChapter(projectID int64, chapter int, instruction string) (*ActionResponse, error) {
	body, err := a.callChapterAction(projectID, chapter, "expand", instruction, 0, false)
	if err != nil {
		return nil, err
	}
	var resp ActionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析 expand 响应失败: %w", err)
	}
	return &resp, nil
}

// RewriteChapter 调 LLM 重写整章 (覆盖).
//
// 后端: POST /api/chapter/{N}/rewrite/  +  body={project_id, instruction}
func (a *App) RewriteChapter(projectID int64, chapter int, instruction string) (*ActionResponse, error) {
	body, err := a.callChapterAction(projectID, chapter, "rewrite", instruction, 0, false)
	if err != nil {
		return nil, err
	}
	var resp ActionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析 rewrite 响应失败: %w", err)
	}
	return &resp, nil
}

// ReviewChapter 调 LLM review 章节 (不改文件, 只评估).
//
// 后端: POST /api/chapter/{N}/review/  +  body={project_id}
// 返回: ReviewResult (含 quality_score + critical/major/minor issues)
func (a *App) ReviewChapter(projectID int64, chapter int) (*ReviewResult, error) {
	body, err := a.callChapterAction(projectID, chapter, "review", "", 0, true)
	if err != nil {
		return nil, err
	}
	var resp ReviewResult
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析 review 响应失败: %w", err)
	}
	return &resp, nil
}

// InsertChapter 调 LLM 在指定行插入新段落.
//
// 后端: POST /api/chapter/{N}/insert/  +  body={project_id, instruction, position}
// position: 1-based 行号 (1=开头, len(lines)+1=末尾)
func (a *App) InsertChapter(projectID int64, chapter int, position int, instruction string) (*ActionResponse, error) {
	if position <= 0 {
		return nil, fmt.Errorf("position 必须 > 0 (1-based 行号)")
	}
	body, err := a.callChapterAction(projectID, chapter, "insert", instruction, position, false)
	if err != nil {
		return nil, err
	}
	var resp ActionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析 insert 响应失败: %w", err)
	}
	return &resp, nil
}

// RollbackChapter 从最新 .bak.* 备份恢复章节.
//
// 后端: POST /api/chapter/{N}/rollback/  +  body={project_id}
//
// 注意: 没有 backup 参数, 后端自动找最新 .bak.{unix_timestamp}.
func (a *App) RollbackChapter(projectID int64, chapter int) (*ActionResponse, error) {
	body, err := a.callChapterAction(projectID, chapter, "rollback", "", 0, false)
	if err != nil {
		return nil, err
	}
	var resp ActionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析 rollback 响应失败: %w", err)
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Module D: 知识管理 (人物 / 关系 / 伏笔) — CRUD wails-bound 方法
// ---------------------------------------------------------------------------

// callKnowledgeCRUD 通用 helper, 15 个 wails 方法都走这里.
//
// category: "characters" / "relationships" / "foreshadows"
// method:   GET (list) / POST (create) / GET/ID (get) / PUT/ID (update) / DELETE/ID (delete)
// path:     /api/{category} 或 /api/{category}/{id}
// body:     nil for GET/DELETE, marshalled JSON for POST/PUT
// result:   unmarshal target (ptr to struct/slice)
//
// 返回值: HTTP 状态码非 200/201/204 → 已 wrap 的 error; 否则 nil.
func (a *App) callKnowledgeCRUD(method, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	resp, err := a.doRequest(method, path, reqBody)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 204 No Content (DELETE 成功) → 无 body, result 应 nil
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s 失败 (HTTP %d): %s", method, path, resp.StatusCode, string(body))
	}
	// 读取 body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	// result nil 表示调用方不关心 body (如 DELETE)
	if result == nil {
		return nil
	}
	if len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("解析响应失败: %w (body: %s)", err, string(respBody))
	}
	return nil
}

// --- Characters (3 list/get/create + 1 update + 1 delete = 5 methods) ---

func (a *App) ListCharacters(projectID int64) ([]Character, error) {
	path := fmt.Sprintf("/api/characters?project_root=.")
	var out struct {
		Characters []Character `json:"characters"`
		Count      int          `json:"count"`
	}
	if err := a.callKnowledgeCRUD(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Characters, nil
}

func (a *App) GetCharacter(projectID int64, id int64) (*Character, error) {
	path := fmt.Sprintf("/api/characters/%d", id)
	var c Character
	if err := a.callKnowledgeCRUD(http.MethodGet, path, nil, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (a *App) CreateCharacter(input Character) (*Character, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("character name 不能为空")
	}
	// 后端不收 project_root (fallback "."), 但 ID 由后端分配
	input.ID = 0
	var c Character
	if err := a.callKnowledgeCRUD(http.MethodPost, "/api/characters", input, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (a *App) UpdateCharacter(id int64, input Character) (*Character, error) {
	if id <= 0 {
		return nil, fmt.Errorf("character id 必须 > 0")
	}
	input.ID = id // 强制 ID 与 path 一致
	var c Character
	path := fmt.Sprintf("/api/characters/%d", id)
	if err := a.callKnowledgeCRUD(http.MethodPut, path, input, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (a *App) DeleteCharacter(id int64) error {
	if id <= 0 {
		return fmt.Errorf("character id 必须 > 0")
	}
	path := fmt.Sprintf("/api/characters/%d", id)
	return a.callKnowledgeCRUD(http.MethodDelete, path, nil, nil)
}

// --- Relationships (5 methods, 同 pattern) ---

func (a *App) ListRelationships(projectID int64) ([]Relationship, error) {
	path := "/api/relationships?project_root=."
	var out struct {
		Relationships []Relationship `json:"relationships"`
		Count        int             `json:"count"`
	}
	if err := a.callKnowledgeCRUD(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Relationships, nil
}

func (a *App) GetRelationship(projectID int64, id int64) (*Relationship, error) {
	path := fmt.Sprintf("/api/relationships/%d", id)
	var r Relationship
	if err := a.callKnowledgeCRUD(http.MethodGet, path, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (a *App) CreateRelationship(input Relationship) (*Relationship, error) {
	if input.CharacterA == "" || input.CharacterB == "" {
		return nil, fmt.Errorf("character_a 和 character_b 必填")
	}
	input.ID = 0
	var r Relationship
	if err := a.callKnowledgeCRUD(http.MethodPost, "/api/relationships", input, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (a *App) UpdateRelationship(id int64, input Relationship) (*Relationship, error) {
	if id <= 0 {
		return nil, fmt.Errorf("relationship id 必须 > 0")
	}
	input.ID = id
	var r Relationship
	path := fmt.Sprintf("/api/relationships/%d", id)
	if err := a.callKnowledgeCRUD(http.MethodPut, path, input, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (a *App) DeleteRelationship(id int64) error {
	if id <= 0 {
		return fmt.Errorf("relationship id 必须 > 0")
	}
	path := fmt.Sprintf("/api/relationships/%d", id)
	return a.callKnowledgeCRUD(http.MethodDelete, path, nil, nil)
}

// --- Foreshadows (5 methods, 同 pattern) ---

func (a *App) ListForeshadows(projectID int64) ([]Foreshadow, error) {
	path := "/api/foreshadows?project_root=."
	var out struct {
		Foreshadows []Foreshadow `json:"foreshadows"`
		Count      int           `json:"count"`
	}
	if err := a.callKnowledgeCRUD(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Foreshadows, nil
}

func (a *App) GetForeshadow(projectID int64, id int64) (*Foreshadow, error) {
	path := fmt.Sprintf("/api/foreshadows/%d", id)
	var f Foreshadow
	if err := a.callKnowledgeCRUD(http.MethodGet, path, nil, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (a *App) CreateForeshadow(input Foreshadow) (*Foreshadow, error) {
	if input.Title == "" || input.PlantedChapter <= 0 {
		return nil, fmt.Errorf("title 和 planted_chapter 必填")
	}
	input.ID = 0
	var f Foreshadow
	if err := a.callKnowledgeCRUD(http.MethodPost, "/api/foreshadows", input, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (a *App) UpdateForeshadow(id int64, input Foreshadow) (*Foreshadow, error) {
	if id <= 0 {
		return nil, fmt.Errorf("foreshadow id 必须 > 0")
	}
	input.ID = id
	var f Foreshadow
	path := fmt.Sprintf("/api/foreshadows/%d", id)
	if err := a.callKnowledgeCRUD(http.MethodPut, path, input, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (a *App) DeleteForeshadow(id int64) error {
	if id <= 0 {
		return fmt.Errorf("foreshadow id 必须 > 0")
	}
	path := fmt.Sprintf("/api/foreshadows/%d", id)
	return a.callKnowledgeCRUD(http.MethodDelete, path, nil, nil)
}

// ---------------------------------------------------------------------------
// Module E: 章节大纲 (Outline) CRUD
// ---------------------------------------------------------------------------

// callOutlineCRUD 通用 helper. 后端 /api/outline + /api/outline/{id}.
func (a *App) callOutlineCRUD(method, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	resp, err := a.doRequest(method, path, reqBody)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s 失败 (HTTP %d): %s", method, path, resp.StatusCode, string(respBody))
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if result == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("解析响应失败: %w (body: %s)", err, string(respBody))
	}
	return nil
}

func (a *App) ListOutlines(projectID int64) ([]OutlineItem, error) {
	var out struct {
		Outline []OutlineItem `json:"outline"`
		Count   int            `json:"count"`
	}
	if err := a.callOutlineCRUD(http.MethodGet, "/api/outline", nil, &out); err != nil {
		return nil, err
	}
	// 按章节号排序 (后端不保证顺序)
	items := out.Outline
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].Chapter < items[i].Chapter {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	return items, nil
}

func (a *App) GetOutline(projectID int64, id int64) (*OutlineItem, error) {
	path := fmt.Sprintf("/api/outline/%d", id)
	var item OutlineItem
	if err := a.callOutlineCRUD(http.MethodGet, path, nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *App) CreateOutline(input OutlineItem) (*OutlineItem, error) {
	if input.Chapter <= 0 {
		return nil, fmt.Errorf("章节号必须 > 0")
	}
	if input.Title == "" {
		return nil, fmt.Errorf("章节标题必填")
	}
	input.ID = 0 // 后端分配
	var item OutlineItem
	if err := a.callOutlineCRUD(http.MethodPost, "/api/outline", input, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *App) UpdateOutline(id int64, input OutlineItem) (*OutlineItem, error) {
	if id <= 0 {
		return nil, fmt.Errorf("outline id 必须 > 0")
	}
	if input.Chapter <= 0 {
		return nil, fmt.Errorf("章节号必须 > 0")
	}
	if input.Title == "" {
		return nil, fmt.Errorf("章节标题必填")
	}
	input.ID = id
	var item OutlineItem
	path := fmt.Sprintf("/api/outline/%d", id)
	if err := a.callOutlineCRUD(http.MethodPut, path, input, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *App) DeleteOutline(id int64) error {
	if id <= 0 {
		return fmt.Errorf("outline id 必须 > 0")
	}
	path := fmt.Sprintf("/api/outline/%d", id)
	return a.callOutlineCRUD(http.MethodDelete, path, nil, nil)
}

// ---------------------------------------------------------------------------
// Module H: Skills 调用 (LLM 驱动的 13 个技能)
// ---------------------------------------------------------------------------

// callSkillsExecute 通用 helper (GET list + POST execute-sync).
func (a *App) callSkillsExecute(method, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	resp, err := a.doRequest(method, path, reqBody)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s 失败 (HTTP %d): %s", method, path, resp.StatusCode, string(respBody))
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if result == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("解析响应失败: %w (body: %s)", err, string(respBody))
	}
	return nil
}

// ListSkills 列出所有 13 个 skill (name + description).
//
// 后端 GET /api/skills → {skills: [...], count: N}
func (a *App) ListSkills() ([]SkillSummary, error) {
	var out struct {
		Skills []SkillSummary `json:"skills"`
		Count  int             `json:"count"`
	}
	if err := a.callSkillsExecute(http.MethodGet, "/api/skills", nil, &out); err != nil {
		return nil, err
	}
	return out.Skills, nil
}

// ExecuteSkillSync 同步执行 skill (阻塞等结果).
//
// 后端 POST /api/skills/{name}/execute-sync → SkillExecuteResult
// 注意: 同步调用可能耗时 10-60s (LLM 调用). timeout 在 doRequest 里 = 600s.
//
// Input: 用户 prompt
// Provider/Model/Variables: 可选覆盖 (空 = 后端默认)
func (a *App) ExecuteSkillSync(name string, input string, provider string, model string) (*SkillExecuteResult, error) {
	if name == "" {
		return nil, fmt.Errorf("skill name 不能为空")
	}
	if input == "" {
		return nil, fmt.Errorf("input 不能为空")
	}
	body := map[string]any{
		"input":    input,
		"provider": provider,
		"model":    model,
	}
	path := fmt.Sprintf("/api/skills/%s/execute-sync", name)
	var result SkillExecuteResult
	if err := a.callSkillsExecute(http.MethodPost, path, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSkill 单个 skill 的详细信息 (description + references 等).
//
// 后端 GET /api/skills/{name} → 返回 Skill struct (body + references 列表).
// 注: 后端当前没有此 endpoint (需走 skillsHandler.ListDetailed 或类似).
// 暂用 ListSkills 过滤, 后续可加独立端点.
func (a *App) GetSkill(name string) (*SkillSummary, error) {
	skills, err := a.ListSkills()
	if err != nil {
		return nil, err
	}
	for _, s := range skills {
		if s.Name == name {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("skill %q not found", name)
}
