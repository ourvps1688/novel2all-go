package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

func setupTestAuthWithAdmin(t *testing.T) (*AuthHandler, *store.DB) {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 创建 admin + 普通用户
	adminHash, _ := auth.HashPassword("adminpass")
	if _, err := db.CreateUser(ctx, "admin", adminHash, "admin"); err != nil {
		t.Fatal(err)
	}
	userHash, _ := auth.HashPassword("userpass")
	if _, err := db.CreateUser(ctx, "alice", userHash, "user"); err != nil {
		t.Fatal(err)
	}

	sm := auth.NewSessionManager(db, auth.DefaultSessionConfig())
	limiter := auth.NewRateLimiter(5, 1*time.Minute, 30*time.Second)
	return NewAuthHandler(sm, limiter), db
}

// TestRegister_Basic 测试基本注册
func TestRegister_Basic(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	body, _ := json.Marshal(RegisterRequest{
		Username: "newuser1",
		Password: "pass1234",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("期望 201，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp RegisterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Username != "newuser1" {
		t.Errorf("username=%q", resp.Username)
	}
	if resp.Role != "user" {
		t.Errorf("role 应为 user（register 强制）=%q", resp.Role)
	}
}

// TestRegister_DuplicateUsername 测试重名返回 409
func TestRegister_DuplicateUsername(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	body, _ := json.Marshal(RegisterRequest{Username: "alice", Password: "pass1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("重名应 409，实际=%d", rec.Code)
	}
}

// TestRegister_ShortPassword 测试密码太短
func TestRegister_ShortPassword(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	body, _ := json.Marshal(RegisterRequest{Username: "newuser2", Password: "123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("短密码应 400，实际=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestListUsers_RequiresAdmin 测试列表需要 admin
func TestListUsers_RequiresAdmin(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	// 无 cookie → 401
	req := httptest.NewRequest(http.MethodGet, "/api/auth/users", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 cookie 应 401，实际=%d", rec.Code)
	}
}

// TestListUsers_AsAdmin 测试 admin 列表
func TestListUsers_AsAdmin(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	// 登录拿 admin cookie
	loginBody, _ := json.Marshal(LoginRequest{Username: "admin", Password: "adminpass"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("登录失败")
	}

	// 带 cookie 列表
	req := httptest.NewRequest(http.MethodGet, "/api/auth/users", http.NoBody)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("admin 应 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp UsersListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 2 { // admin + alice
		t.Errorf("应 2 个用户，实际=%d", resp.Count)
	}
}

// TestCreateUser_AsAdmin 测试 admin 创建新用户（可指定 role）
func TestCreateUser_AsAdmin(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	// 拿 admin cookie
	loginBody, _ := json.Marshal(LoginRequest{Username: "admin", Password: "adminpass"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	cookies := loginRec.Result().Cookies()

	// admin 创建另一个 admin
	body, _ := json.Marshal(RegisterRequest{
		Username: "admin2",
		Password: "pass1234",
		Role:     "admin",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/users", bytes.NewReader(body))
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("admin 创建应 201，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp RegisterResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Role != "admin" {
		t.Errorf("admin 创建的 role 应为 admin，实际=%q", resp.Role)
	}
}

// TestDeleteUser_SelfDelete 测试自删防护
func TestDeleteUser_SelfDelete(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	// 拿 admin cookie
	loginBody, _ := json.Marshal(LoginRequest{Username: "admin", Password: "adminpass"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	cookies := loginRec.Result().Cookies()

	// admin 删自己（带 trailing slash 触发 subtree handler）
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/users/1/", http.NoBody)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("自删应 400，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "yourself") {
		t.Errorf("错误信息应含 'yourself'")
	}
}

// TestRolesList 测试 /api/roles
func TestRolesList(t *testing.T) {
	h := NewRolesHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/roles", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d", rec.Code)
	}
	var resp RolesListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 5 {
		t.Errorf("应 5 个角色，实际=%d", resp.Count)
	}
}

// TestCacheStats 测试 cache 统计
func TestCacheStats(t *testing.T) {
	h := NewCacheHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/cache/stats", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var stats CacheStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.Backend != "memory" {
		t.Errorf("backend 应为 memory，实际=%q", stats.Backend)
	}
}

// TestPromptCacheStats 测试 prompt prefix cache 统计
func TestPromptCacheStats(t *testing.T) {
	h := NewCacheHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/cache/prompt-stats", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var stats PromptCacheStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.HitRate != 0 {
		t.Errorf("初始 hit_rate 应为 0，实际=%f", stats.HitRate)
	}
}