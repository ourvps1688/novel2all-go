package auth

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

func setupTestSessionManager(t *testing.T) (*SessionManager, *store.DB) {
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
	return NewSessionManager(db, DefaultSessionConfig()), db
}

func TestSessionManager_LoginSuccess(t *testing.T) {
	ctx := context.Background()
	sm, db := setupTestSessionManager(t)

	// 创建用户
	hash, _ := HashPassword("pass1234")
	userID, _ := db.CreateUser(ctx, "testuser", hash, "user")

	// 登录
	token, u, err := sm.Login(ctx, "testuser", "pass1234", "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token == "" {
		t.Error("token 不应为空")
	}
	if u.ID != userID {
		t.Errorf("user ID 不匹配")
	}

	// 验证 session
	got, err := sm.GetUserByToken(ctx, token)
	if err != nil {
		t.Fatalf("get by token: %v", err)
	}
	if got.Username != "testuser" {
		t.Errorf("username 不匹配")
	}
}

func TestSessionManager_LoginBadPassword(t *testing.T) {
	ctx := context.Background()
	sm, db := setupTestSessionManager(t)

	hash, _ := HashPassword("correct")
	_, _ = db.CreateUser(ctx, "user", hash, "user")

	_, _, err := sm.Login(ctx, "user", "wrong", "127.0.0.1", "")
	if err == nil {
		t.Error("错误密码应报错")
	}
}

func TestSessionManager_LoginDisabled(t *testing.T) {
	ctx := context.Background()
	sm, db := setupTestSessionManager(t)

	hash, _ := HashPassword("pass")
	id, _ := db.CreateUser(ctx, "user", hash, "user")
	_ = db.SetUserDisabled(ctx, id, true)

	_, _, err := sm.Login(ctx, "user", "pass", "127.0.0.1", "")
	if err == nil {
		t.Error("禁用用户登录应报错")
	}
}

func TestSessionManager_Logout(t *testing.T) {
	ctx := context.Background()
	sm, db := setupTestSessionManager(t)

	hash, _ := HashPassword("pass")
	_, _ = db.CreateUser(ctx, "user", hash, "user")
	token, _, _ := sm.Login(ctx, "user", "pass", "127.0.0.1", "")

	// 登出
	if err := sm.Logout(ctx, token, "127.0.0.1", ""); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// token 应该失效
	_, err := sm.GetUserByToken(ctx, token)
	if err == nil {
		t.Error("logout 后 token 应失效")
	}
}

func TestSessionManager_SetAndGetCookie(t *testing.T) {
	sm, _ := setupTestSessionManager(t)

	// 写 cookie
	rec := httptest.NewRecorder()
	sm.SetCookie(rec, "test-token-xyz")
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("Set-Cookie 应被设置")
	}
	// 应包含 HttpOnly
	result := rec.Result()
	defer result.Body.Close()
	cookie := result.Cookies()
	if len(cookie) == 0 {
		t.Fatal("应至少有 1 个 cookie")
	}
	if !cookie[0].HttpOnly {
		t.Error("cookie 应为 HttpOnly")
	}
	if cookie[0].Value != "test-token-xyz" {
		t.Errorf("cookie value 不匹配: %q", cookie[0].Value)
	}
}

func TestSessionManager_SessionExpired(t *testing.T) {
	ctx := context.Background()
	db, _ := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	_ = db.Migrate(ctx)
	defer db.Close()

	// 用 1ms TTL 创建 session
	sm := NewSessionManager(db, SessionConfig{
		CookieName: "x",
		TTL:        1 * time.Millisecond,
		SameSite:   0,
	})
	hash, _ := HashPassword("p")
	db.CreateUser(ctx, "u", hash, "user")
	token, _, _ := sm.Login(ctx, "u", "p", "", "")

	// 等待过期
	time.Sleep(50 * time.Millisecond)

	_, err := sm.GetUserByToken(ctx, token)
	if err == nil {
		t.Error("过期 session 应被拒绝")
	}
}
