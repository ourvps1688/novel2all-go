package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// fakeUserLookup 实现 UserLookup 接口, 用于单元测试 RequireAuth.
//
// 用 map[token]*store.User 模拟 session store, 不依赖 SQLite.
type fakeUserLookup struct {
	users      map[string]*store.User
	cookieName string
}

// newFakeLookup 构造 fake, 包含 1 个 admin (token=admin-tok) + 1 个 user (token=user-tok).
func newFakeLookup() *fakeUserLookup {
	now := time.Now()
	return &fakeUserLookup{
		cookieName: "novel2all_session",
		users: map[string]*store.User{
			"admin-tok": {
				ID:        1,
				Username:  "admin1",
				Role:      roleAdmin,
				Disabled:  false,
				CreatedAt: now,
			},
			"user-tok": {
				ID:        2,
				Username:  "alice",
				Role:      "user",
				Disabled:  false,
				CreatedAt: now,
			},
			"disabled-tok": {
				ID:        3,
				Username:  "bob_disabled",
				Role:      "user",
				Disabled:  true,
				CreatedAt: now,
			},
		},
	}
}

func (f *fakeUserLookup) GetTokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(f.cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (f *fakeUserLookup) GetUserByToken(_ context.Context, token string) (*store.User, error) {
	u, ok := f.users[token]
	if !ok {
		return nil, auth.ErrUnauthorized
	}
	if u.Disabled {
		return nil, auth.ErrUnauthorized
	}
	return u, nil
}

// TestRequireAuth_NoCookie_Returns401 验证: 无 cookie → 401 + 不调 next.
func TestRequireAuth_NoCookie_Returns401(t *testing.T) {
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(lookup)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("期望 401, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("next 不应被调用")
	}
}

// TestRequireAuth_ValidCookie_OK 验证: 带有效 cookie → 200 + user 注入 context.
func TestRequireAuth_ValidCookie_OK(t *testing.T) {
	lookup := newFakeLookup()
	var seenUser *store.User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUser, _ = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	handler := RequireAuth(lookup)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-tok"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if seenUser == nil {
		t.Fatal("next 应能从 context 拿到 user")
	}
	if seenUser.Username != "alice" {
		t.Errorf("user.Username=%q, want alice", seenUser.Username)
	}
	if seenUser.Role != "user" {
		t.Errorf("user.Role=%q, want user", seenUser.Role)
	}
}

// TestRequireAuth_AdminCookie_OK 验证: admin user 也通过 (RequireAuth 不做角色判断).
func TestRequireAuth_AdminCookie_OK(t *testing.T) {
	lookup := newFakeLookup()
	var seenUser *store.User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUser, _ = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(lookup)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-tok"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", rec.Code)
	}
	if seenUser == nil || seenUser.Role != roleAdmin {
		t.Errorf("admin user 应被注入 context, got=%+v", seenUser)
	}
}

// TestRequireAuth_InvalidCookie_Returns401 验证: 伪造 token → 401.
func TestRequireAuth_InvalidCookie_Returns401(t *testing.T) {
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(lookup)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "fake-fake-fake"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("期望 401, 实际 %d", rec.Code)
	}
	if called {
		t.Error("伪造 token 不应通过")
	}
}

// TestRequireAuth_DisabledUser_Returns401 验证: user.Disabled=true → 401 (与 SessionManager 行为一致).
func TestRequireAuth_DisabledUser_Returns401(t *testing.T) {
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(lookup)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "disabled-tok"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("期望 401 (disabled user), 实际 %d", rec.Code)
	}
	if called {
		t.Error("disabled user 不应被允许")
	}
}

// TestRequireAuth_NilSession_Returns503 验证: session=nil → 503 (与 RequireAdmin fallback 对齐).
func TestRequireAuth_NilSession_Returns503(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-tok"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("期望 503, 实际 %d", rec.Code)
	}
	if called {
		t.Error("session=nil 不应调 next")
	}
}

// TestRequireAuth_EmptyCookie_Returns401 验证: cookie Value="" → 401 (空 token 也视为未认证).
func TestRequireAuth_EmptyCookie_Returns401(t *testing.T) {
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(lookup)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: ""})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("期望 401, 实际 %d", rec.Code)
	}
	if called {
		t.Error("空 token 不应通过")
	}
}

// TestRequireAuth_ResponseBodyIsJSON 验证: 401 响应体是合法 JSON `{"error":"..."}`.
//
// 不是严格的 schema 检查, 但保证前端 fetch().json() 不报错.
func TestRequireAuth_ResponseBodyIsJSON(t *testing.T) {
	lookup := newFakeLookup()
	handler := RequireAuth(lookup)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if body == "" {
		t.Fatal("401 响应体不应为空")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Errorf("401 响应体非合法 JSON: %v body=%s", err, body)
	}
	if _, ok := parsed["error"]; !ok {
		t.Errorf("401 响应体应含 'error' 字段, got=%s", body)
	}
}

// TestUserFromContext_NotSet_ReturnsNilFalse 验证: 无 user 注入时返回 (nil, false).
func TestUserFromContext_NotSet_ReturnsNilFalse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	u, ok := UserFromContext(req.Context())
	if u != nil || ok {
		t.Errorf("期望 (nil, false), got (%+v, %v)", u, ok)
	}
}

// TestUserFromContext_RoundTrip 验证: UserFromContext 与 RequireAuth 注入 round-trip 一致.
func TestUserFromContext_RoundTrip(t *testing.T) {
	want := &store.User{ID: 42, Username: "roundtrip", Role: "user"}
	ctx := context.WithValue(context.Background(), userCtxValue, want)
	got, ok := UserFromContext(ctx)
	if !ok {
		t.Fatal("UserFromContext 应返回 ok=true")
	}
	if got != want {
		t.Errorf("got=%+v, want=%+v", got, want)
	}
}

// TestUserFromContext_WrongType_ReturnsNilFalse 验证: context 中存的是别的类型时, 不强转, 安全返回 (nil, false).
//
// 防止其他 middleware (例如 debug, audit) 错误覆盖 userCtxKey 时 panic.
func TestUserFromContext_WrongType_ReturnsNilFalse(t *testing.T) {
	ctx := context.WithValue(context.Background(), userCtxValue, "not-a-user")
	u, ok := UserFromContext(ctx)
	if u != nil || ok {
		t.Errorf("类型不匹配应返回 (nil, false), got (%+v, %v)", u, ok)
	}
}

// TestMustUserFromContext_PanicsOnMissing 验证: ctx 无 user → panic (防止 router 配错时静默).
func TestMustUserFromContext_PanicsOnMissing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("期望 panic, 实际没 panic")
		}
	}()
	_ = MustUserFromContext(context.Background())
}

// TestRequireAuth_DoesNotMutateRequestPath 验证: 鉴权失败时, request 不应被改写 (path/header 保持原样).
func TestRequireAuth_DoesNotMutateRequestPath(t *testing.T) {
	lookup := newFakeLookup()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// next 仅在成功路径调用 — 这里不会被触发
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(lookup)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/projects?owner=1", http.NoBody)
	req.Header.Set("X-Test", "original")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("期望 401, got %d", rec.Code)
	}
	// 无 cookie → next 没被调用, 不会动 request. 但要保证错误响应没意外改了 header.
	if rec.Header().Get("X-Test") != "" {
		t.Errorf("401 响应不应携带 X-Test, got=%q", rec.Header().Get("X-Test"))
	}
}

// TestRequireAuth_LookupErrorIs401 验证: GetUserByToken 返回非 nil error 时也 401.
//
// 防止某些 UserLookup 实现 (mock 或 future) 返回其他 error 类型 (e.g. ErrUserNotFound)
// 时被错误地当成成功.
func TestRequireAuth_LookupErrorIs401(t *testing.T) {
	lookup := &fakeUserLookup{
		cookieName: "novel2all_session",
		users:      map[string]*store.User{}, // 空 map → 所有 token 都返回 auth.ErrUnauthorized
	}
	handler := RequireAuth(lookup)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "anything"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("lookup error 应 401, got %d", rec.Code)
	}
}

// 编译期断言: fakeUserLookup 实现 UserLookup 接口.
var _ UserLookup = (*fakeUserLookup)(nil)

// =====================================================================
// Sprint V1.0.1 P3 RequireAdmin mux-level middleware tests
// =====================================================================

// requestWithUser 构造带 user context 的 request (用于 RequireAdmin 测试).
//
// RequireAdmin 从 context 取 user (假设已被前置 RequireAuth 注入),
// 不读 cookie, 所以测试需手动构造 user context.
func requestWithUser(method, url string, user *store.User) *http.Request {
	req := httptest.NewRequest(method, url, http.NoBody)
	if user != nil {
		ctx := context.WithValue(req.Context(), userCtxValue, user)
		req = req.WithContext(ctx)
	}
	return req
}

// TestRequireAdmin_NoUser_Returns401 验证: user 不在 context → 401 (前置 RequireAuth 没注入).
func TestRequireAdmin_NoUser_Returns401(t *testing.T) {
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAdmin(lookup)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody)
	// 不注入 user
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 user 应 401, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("next 不应被调用")
	}
}

// TestRequireAdmin_AdminUser_OK 验证: admin user → 调 next.
func TestRequireAdmin_AdminUser_OK(t *testing.T) {
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAdmin(lookup)(next)

	admin := &store.User{ID: 1, Username: "admin1", Role: "admin"}
	req := requestWithUser(http.MethodGet, "/api/state", admin)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("admin 应 200, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Error("next 应被调用")
	}
}

// TestRequireAdmin_RegularUser_Returns403 验证: 普通 user → 403.
func TestRequireAdmin_RegularUser_Returns403(t *testing.T) {
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAdmin(lookup)(next)

	regular := &store.User{ID: 2, Username: "alice", Role: "user"}
	req := requestWithUser(http.MethodGet, "/api/state", regular)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("普通 user 应 403, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("普通 user 不应调 next")
	}
}

// TestRequireAdmin_NilSession_Returns503 验证: session=nil → 503 (与 RequireAuth fallback 对齐).
func TestRequireAdmin_NilSession_Returns503(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAdmin(nil)(next)

	admin := &store.User{ID: 1, Role: "admin"}
	req := requestWithUser(http.MethodGet, "/api/state", admin)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("session=nil 应 503, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("session=nil 不应调 next")
	}
}

// TestRequireAdmin_DisabledUser_StillRequiresAdmin 验证: disabled user 仍被 RequireAdmin 当作 401 (前置 RequireAuth 已拦截).
//
// 注: disabled user 在 RequireAuth 已返回 401, 不应到 RequireAdmin 这一层.
// 但如果 context 已被手动注入 disabled user (例如测试场景), RequireAdmin 仍正常工作
// (只检查 role, 不查 disabled).
func TestRequireAdmin_DisabledUser_StillPassesAdmin(t *testing.T) {
	// 此测试验证 RequireAdmin 只检查 role, 不检查 disabled.
	// (disabled 检查是 RequireAuth 的责任)
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAdmin(lookup)(next)

	disabledAdmin := &store.User{ID: 3, Username: "admin_disabled", Role: "admin", Disabled: true}
	req := requestWithUser(http.MethodGet, "/api/state", disabledAdmin)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// disabled admin 仍可过 RequireAdmin (它只检查 role)
	if rec.Code != http.StatusOK {
		t.Errorf("disabled admin 应过 RequireAdmin (Role 仍为 admin), 实际 %d", rec.Code)
	}
	if !called {
		t.Error("next 应被调用")
	}
}

// TestRequireAdmin_ChainWithAuth 验证: RequireAuth + RequireAdmin chain 正常工作.
//
// 模拟真实 router 链式 wrap.
func TestRequireAdmin_ChainWithAuth(t *testing.T) {
	lookup := newFakeLookup()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(lookup)(RequireAdmin(lookup)(next))

	// admin cookie → admin 通过 → 200
	req := httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-tok"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("admin chain 应 200, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Error("next 应被调用")
	}

	// regular user cookie → 403
	called = false
	req = httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-tok"})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("user chain 应 403 (auth 过, admin 拦), 实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("user chain 不应调 next")
	}

	// 无 cookie → 401
	called = false
	req = httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 cookie chain 应 401, 实际 %d", rec.Code)
	}
	if called {
		t.Error("无 cookie chain 不应调 next")
	}
}
