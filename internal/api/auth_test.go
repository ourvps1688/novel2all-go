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
