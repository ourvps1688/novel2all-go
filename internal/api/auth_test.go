package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

func setupTestAuth(t *testing.T) (*AuthHandler, *store.DB) {
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

	// 创建测试用户
	hash, _ := auth.HashPassword("testpass")
	_, _ = db.CreateUser(ctx, "alice", hash, "user")

	sm := auth.NewSessionManager(db, auth.DefaultSessionConfig())
	limiter := auth.NewRateLimiter(5, 1*time.Minute, 30*time.Second)
	return NewAuthHandler(sm, limiter), db
}

func TestAuthHandler_Login(t *testing.T) {
	h, _ := setupTestAuth(t)

	body, _ := json.Marshal(LoginRequest{Username: "alice", Password: "testpass"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	// 应有 Set-Cookie
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("应设置 session cookie")
	}
	var resp LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Username != "alice" {
		t.Errorf("username=%q", resp.Username)
	}
}

func TestAuthHandler_LoginBadPassword(t *testing.T) {
	h, _ := setupTestAuth(t)

	body, _ := json.Marshal(LoginRequest{Username: "alice", Password: "wrongpass"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("错误密码应 401，实际=%d", rec.Code)
	}
}

func TestAuthHandler_LoginMissingFields(t *testing.T) {
	h, _ := setupTestAuth(t)

	body, _ := json.Marshal(LoginRequest{Username: "alice"}) // 缺 password
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("缺字段应 400，实际=%d", rec.Code)
	}
}

func TestAuthHandler_Logout(t *testing.T) {
	h, _ := setupTestAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("期望 204，实际=%d", rec.Code)
	}
}

func TestAuthHandler_Me_Unauthorized(t *testing.T) {
	h, _ := setupTestAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 cookie 应 401，实际=%d", rec.Code)
	}
}

func TestAuthHandler_Me_Authorized(t *testing.T) {
	h, _ := setupTestAuth(t)

	// 先登录拿 cookie
	body, _ := json.Marshal(LoginRequest{Username: "alice", Password: "testpass"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	loginResult := loginRec.Result()
	defer loginResult.Body.Close()
	cookies := loginResult.Cookies()
	if len(cookies) == 0 {
		t.Fatal("登录后应设置 cookie")
	}

	// 带 cookie 调 /me
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", http.NoBody)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
}

// =====================================================================
// Sprint V1.0.1 P0-A: 验证 /api/auth/* 路由公开 (不受 RequireAuth wrap)
//
// 上方 TestAuthHandler_* 测试 AuthHandler 单元; 此处验证 mux-level
// auth flow: register + login + /me 通过完整 Router 走通.
// =====================================================================

// TestAuthMux_FullFlow_RegisterLoginMe 通过完整 api.Router 跑 register → login → /me.
//
// 关键: /api/auth/* 在路由中是 PUBLIC, 不被 RequireAuth wrap.
// 期望所有 endpoint 都 200/201, 不出现 401.
func TestAuthMux_FullFlow_RegisterLoginMe(t *testing.T) {
	mux := setupAuthMuxForProjects(t)

	// 1. register
	body, _ := json.Marshal(RegisterRequest{Username: "v101_user", Password: "v101pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register 应 201, 实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 2. login
	body, _ = json.Marshal(LoginRequest{Username: "v101_user", Password: "v101pass"})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login 应 200, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	loginResp := rec.Result()
	defer func() { _ = loginResp.Body.Close() }()
	cookies := loginResp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("login 未设置 cookie")
	}

	// 3. /me (公开 + cookie)
	req = httptest.NewRequest(http.MethodGet, "/api/auth/me", http.NoBody)
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/me 应 200, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

// setupAuthMuxForProjects 构造最小 mux 用于测试 /api/auth/* + 受保护端点交互.
//
// 复用 projects_test.go 的 setupProjectsAuthMux 风格.
func setupAuthMuxForProjects(t *testing.T) http.Handler {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "test_v101.db")
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sm := auth.NewSessionManager(db, auth.DefaultSessionConfig())
	limiter := auth.NewRateLimiter(5, 1*time.Minute, 30*time.Second)

	deps := Deps{
		Store:   db,
		Session: sm,
		Limiter: limiter,
	}
	deps.SetProjectStore(NewSQLiteProjectsAdapter(store.NewProjectsStore(db)))
	return Router(deps)
}
