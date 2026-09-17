// Package api 提供 novel2all-go HTTP handlers.
package api

// users_test.go 测试 /api/auth/users CRUD (Sprint 17 抽出).
//
// 与 auth_register_test.go 不同: users 端点现在由独立 UsersHandler 处理,
// 测试通过 mux 注册两个 handler, 模拟真实路由行为.
//
// 测试覆盖 (6 个):
//   - TestListUsers_RequiresAdmin: 无 cookie → 401
//   - TestListUsers_AsAdmin: admin cookie → 200, 返回所有用户
//   - TestListUsers_AsUser: 普通用户 → 403
//   - TestCreateUser_AsAdmin: admin 创建新用户 (可指定 role)
//   - TestCreateUser_Duplicate: 重名 → 409
//   - TestDeleteUser_SelfDelete: 自删防护 (admin 不能删自己)
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

// setupUsersTestMux 创建 mux + admin/alice 用户 + session manager.
//
// 返回:
//   - mux: 注册了 /api/auth/* (AuthHandler) + /api/auth/users[/{id}] (UsersHandler)
//   - session manager: 用于手动登录拿 cookie
//   - db: store 引用 (测试可查数据库)
func setupUsersTestMux(t *testing.T) (*http.ServeMux, *auth.SessionManager, *store.DB) {
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

	mux := http.NewServeMux()
	// AuthHandler 负责 login/logout/me/register/audit
	mux.Handle("/api/auth/", NewAuthHandler(sm, limiter))
	// UsersHandler 负责 users CRUD (admin only)
	mux.Handle("/api/auth/users", NewUsersHandler(sm))
	mux.Handle("/api/auth/users/", NewUsersHandler(sm))

	return mux, sm, db
}

// loginAs 模拟登录拿 cookie.
func loginAs(t *testing.T, mux http.Handler, username, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(LoginRequest{Username: username, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录 %s 失败: %d body=%s", username, rec.Code, rec.Body.String())
	}
	result := rec.Result()
	defer func() { _ = result.Body.Close() }()
	cookies := result.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("登录 %s 未返回 cookie", username)
	}
	return cookies[0]
}

// TestListUsers_RequiresAdmin 无 cookie → 401.
func TestListUsers_RequiresAdmin(t *testing.T) {
	mux, _, _ := setupUsersTestMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/users", http.NoBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 cookie 应 401，实际=%d", rec.Code)
	}
}

// TestListUsers_AsAdmin admin cookie → 200, 返回所有用户.
func TestListUsers_AsAdmin(t *testing.T) {
	mux, _, _ := setupUsersTestMux(t)

	cookie := loginAs(t, mux, "admin", "adminpass")
	req := httptest.NewRequest(http.MethodGet, "/api/auth/users", http.NoBody)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin 应 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp UsersListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 2 { // admin + alice
		t.Errorf("应 2 个用户，实际=%d", resp.Count)
	}
}

// TestListUsers_AsUser 普通用户 → 403 (admin only).
func TestListUsers_AsUser(t *testing.T) {
	mux, _, _ := setupUsersTestMux(t)

	cookie := loginAs(t, mux, "alice", "userpass")
	req := httptest.NewRequest(http.MethodGet, "/api/auth/users", http.NoBody)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("普通用户应 403，实际=%d", rec.Code)
	}
}

// TestCreateUser_AsAdmin admin 创建新用户 (可指定 role).
func TestCreateUser_AsAdmin(t *testing.T) {
	mux, _, db := setupUsersTestMux(t)

	cookie := loginAs(t, mux, "admin", "adminpass")
	body, _ := json.Marshal(CreateUserRequest{
		Username: "admin2",
		Password: "pass1234",
		Role:     "admin",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/users", bytes.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("admin 创建应 201，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp CreateUserResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Role != "admin" {
		t.Errorf("admin 创建的 role 应为 admin，实际=%q", resp.Role)
	}
	if resp.Username != "admin2" {
		t.Errorf("username=%q", resp.Username)
	}

	// 验证 DB 真的有这个用户
	ctx := context.Background()
	u, err := db.GetUserByUsername(ctx, "admin2")
	if err != nil {
		t.Fatalf("DB 查询失败: %v", err)
	}
	if u.Role != "admin" {
		t.Errorf("DB role=%q", u.Role)
	}
}

// TestCreateUser_Duplicate 重名 → 409.
func TestCreateUser_Duplicate(t *testing.T) {
	mux, _, _ := setupUsersTestMux(t)

	cookie := loginAs(t, mux, "admin", "adminpass")
	body, _ := json.Marshal(CreateUserRequest{Username: "alice", Password: "pass1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/users", bytes.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("重名应 409，实际=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDeleteUser_SelfDelete admin 自删防护 → 400.
func TestDeleteUser_SelfDelete(t *testing.T) {
	mux, _, _ := setupUsersTestMux(t)

	cookie := loginAs(t, mux, "admin", "adminpass")
	// admin ID=1
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/users/1/", http.NoBody)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("自删应 400，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "yourself") {
		t.Errorf("错误信息应含 'yourself'")
	}
}
