package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
	"github.com/ourvps1688/novel2all-go/internal/testfixtures"
)

func TestProjectStore_CreateAndGet(t *testing.T) {
	s := NewProjectStore()
	p, err := s.Create("My Novel", "my-novel", "desc", 1, "fantasy")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID != 1 || p.Name != "My Novel" {
		t.Errorf("unexpected project: %+v", p)
	}
	got, err := s.Get(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "My Novel" {
		t.Errorf("got.Name = %q, want My Novel", got.Name)
	}
}

func TestProjectStore_DuplicateSlug(t *testing.T) {
	s := NewProjectStore()
	_, _ = s.Create("A", "dup", "", 1, "")
	_, err := s.Create("B", "dup", "", 1, "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate slug error, got %v", err)
	}
}

func TestProjectStore_UpdateAndDelete(t *testing.T) {
	s := NewProjectStore()
	p, _ := s.Create("A", "a", "", 1, "")
	upd, err := s.Update(p.ID, "A2", "new desc", "scifi")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Name != "A2" || upd.Description != "new desc" || upd.Genre != "scifi" {
		t.Errorf("update failed: %+v", upd)
	}
	if err := s.Delete(p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(p.ID); err == nil {
		t.Error("expected not found after delete")
	}
}

func TestProjectsHandler_ListEmpty(t *testing.T) {
	h := NewProjectsHandler()
	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodGet, "/api/projects", http.NoBody, &store.User{ID: 1, Role: "admin"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["count"].(float64) != 0 {
		t.Errorf("count=%v, want 0", resp["count"])
	}
}

// TestProjectsHandler_List_NoUser_Returns401 验证: list handler 无 user context → 401.
//
// Sprint V1.0.1 P0-B: list 现在必须从 context 取 user. 无 user (例如直接调用未注入)
// 视为 unauthenticated. mux-level RequireAuth 已先拦截, 这是 handler 层的双层防御.
func TestProjectsHandler_List_NoUser_Returns401(t *testing.T) {
	h := NewProjectsHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 user 应 401, 实际 %d", rec.Code)
	}
}

// TestProjectsHandler_List_OwnerFilter 验证: 普通 user 只看自己的项目.
//
// alice 创项目, bob 创项目, alice list 只见 alice, bob list 只见 bob, admin list 全部.
func TestProjectsHandler_List_OwnerFilter(t *testing.T) {
	s := NewProjectStore()
	_, _ = s.Create("Alice's Novel", "alice-1", "", 1 /*alice*/, "fantasy")
	_, _ = s.Create("Bob's Novel", "bob-1", "", 2 /*bob*/, "scifi")
	h := &ProjectsHandler{repo: s}

	// alice list → 只见 1 个 (自己的)
	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodGet, "/api/projects", http.NoBody, &store.User{ID: 1, Role: "user"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("alice list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Projects []*Project `json:"projects"`
		Count    int        `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 1 {
		t.Errorf("alice 应见 1 个项目, 实际 %d", resp.Count)
	}
	if len(resp.Projects) > 0 && resp.Projects[0].OwnerID != 1 {
		t.Errorf("alice 见到非自己的项目: %+v", resp.Projects[0])
	}

	// bob list → 只见 1 个 (自己的)
	rec = httptest.NewRecorder()
	req = newRequestAsUser(http.MethodGet, "/api/projects", http.NoBody, &store.User{ID: 2, Role: "user"})
	h.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 1 {
		t.Errorf("bob 应见 1 个项目, 实际 %d", resp.Count)
	}
	if len(resp.Projects) > 0 && resp.Projects[0].OwnerID != 2 {
		t.Errorf("bob 见到非自己的项目: %+v", resp.Projects[0])
	}

	// admin list → 见 2 个 (全部)
	rec = httptest.NewRecorder()
	req = newRequestAsUser(http.MethodGet, "/api/projects", http.NoBody, &store.User{ID: 999, Role: "admin"})
	h.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 2 {
		t.Errorf("admin 应见 2 个项目, 实际 %d", resp.Count)
	}
}

// TestProjectsHandler_List_OwnerFilter_EmptyResult 验证: user 没项目时 list 返回空 (而非全列).
func TestProjectsHandler_List_OwnerFilter_EmptyResult(t *testing.T) {
	s := NewProjectStore()
	// 项目不属于 charlie (id=3)
	_, _ = s.Create("Alice", "alice-1", "", 1, "")
	h := &ProjectsHandler{repo: s}

	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodGet, "/api/projects", http.NoBody, &store.User{ID: 3, Role: "user", Username: "charlie"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 0 {
		t.Errorf("charlie 应见 0 个项目 (全不是他的), 实际 %d", resp.Count)
	}
}

// newRequestAsUser 创建带 *store.User context 的 request (用于直接 handler 测试).
//
// 复用 auth_middleware.go 的 userCtxValue (同包, 可访问未导出 key).
// 用法: req := newRequestAsUser(http.MethodGet, "/api/projects", nil, &store.User{ID: 1, Role: "admin"})
func newRequestAsUser(method, url string, body io.Reader, user *store.User) *http.Request {
	req := httptest.NewRequest(method, url, body)
	if user != nil {
		ctx := context.WithValue(req.Context(), userCtxValue, user)
		req = req.WithContext(ctx)
	}
	return req
}

func TestProjectsHandler_CreateAndGet(t *testing.T) {
	h := NewProjectsHandler()
	alice := &store.User{ID: 1, Role: "user", Username: "alice"}

	// POST create
	body, _ := json.Marshal(map[string]string{
		"name":  "Test Novel",
		"slug":  "test-novel",
		"genre": "fantasy",
	})
	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodPost, "/api/projects", bytes.NewReader(body), alice)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	var p Project
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.OwnerID != alice.ID {
		t.Errorf("created project OwnerID=%d, want %d (P0-B 从 context 注入)", p.OwnerID, alice.ID)
	}

	// GET (alice 读自己的项目, owner check pass)
	rec = httptest.NewRecorder()
	req = newRequestAsUser(http.MethodGet, "/api/projects/1/", http.NoBody, alice)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d", rec.Code)
	}
	var got Project
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "Test Novel" {
		t.Errorf("got name=%q", got.Name)
	}
}

func TestProjectsHandler_GetNotFound(t *testing.T) {
	h := NewProjectsHandler()
	admin := &store.User{ID: 999, Role: "admin", Username: "admin"}

	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodGet, "/api/projects/999/", http.NoBody, admin)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestProjectsHandler_UpdateAndDelete(t *testing.T) {
	h := NewProjectsHandler()
	alice := &store.User{ID: 1, Role: "user", Username: "alice"}

	// Create
	body, _ := json.Marshal(map[string]string{"name": "A", "slug": "a"})
	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodPost, "/api/projects", bytes.NewReader(body), alice)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var p Project
	_ = json.Unmarshal(rec.Body.Bytes(), &p)

	// Update (alice 改自己的项目)
	body, _ = json.Marshal(map[string]string{"name": "A-updated", "genre": "scifi"})
	rec = httptest.NewRecorder()
	req = newRequestAsUser(http.MethodPut, "/api/projects/1/", bytes.NewReader(body), alice)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var upd Project
	_ = json.Unmarshal(rec.Body.Bytes(), &upd)
	if upd.Name != "A-updated" || upd.Genre != "scifi" {
		t.Errorf("update failed: %+v", upd)
	}

	// Delete (alice 删自己的项目)
	rec = httptest.NewRecorder()
	req = newRequestAsUser(http.MethodDelete, "/api/projects/1/", http.NoBody, alice)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status=%d, want 204", rec.Code)
	}
}

// =====================================================================
// Sprint V1.0.1 P0-B CRUD owner isolation tests
//
// 验证: alice 创建的项目, bob 不能 get/update/delete.
// admin 可以跨 user 操作.
// =====================================================================

// TestProjectsHandler_Get_OwnerIsolation 验证: bob 不能 get alice 的项目.
func TestProjectsHandler_Get_OwnerIsolation(t *testing.T) {
	s := NewProjectStore()
	_, _ = s.Create("Alice's Novel", "alice-1", "", 1 /*alice*/, "")
	h := &ProjectsHandler{repo: s}

	alice := &store.User{ID: 1, Role: "user", Username: "alice"}
	bob := &store.User{ID: 2, Role: "user", Username: "bob"}
	admin := &store.User{ID: 999, Role: "admin", Username: "admin"}

	// alice 看自己 → 200
	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodGet, "/api/projects/1/", http.NoBody, alice)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("alice 看自己应 200, 实际 %d", rec.Code)
	}

	// bob 看 alice 的 → 403
	rec = httptest.NewRecorder()
	req = newRequestAsUser(http.MethodGet, "/api/projects/1/", http.NoBody, bob)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob 看 alice 应 403, 实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// admin 看 alice 的 → 200 (admin bypass)
	rec = httptest.NewRecorder()
	req = newRequestAsUser(http.MethodGet, "/api/projects/1/", http.NoBody, admin)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("admin 看 alice 应 200, 实际 %d", rec.Code)
	}
}

// TestProjectsHandler_Update_OwnerIsolation 验证: bob 不能 update alice 的项目.
func TestProjectsHandler_Update_OwnerIsolation(t *testing.T) {
	s := NewProjectStore()
	_, _ = s.Create("Alice's Novel", "alice-1", "", 1, "")
	h := &ProjectsHandler{repo: s}

	bob := &store.User{ID: 2, Role: "user", Username: "bob"}

	body, _ := json.Marshal(map[string]string{"name": "Hacked"})
	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodPut, "/api/projects/1/", bytes.NewReader(body), bob)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob update alice 应 403, 实际 %d", rec.Code)
	}

	// 验证项目未被修改
	p, _ := s.Get(1)
	if p.Name == "Hacked" {
		t.Errorf("bob 越权 update 成功, name=%q", p.Name)
	}
}

// TestProjectsHandler_Delete_OwnerIsolation 验证: bob 不能 delete alice 的项目.
func TestProjectsHandler_Delete_OwnerIsolation(t *testing.T) {
	s := NewProjectStore()
	_, _ = s.Create("Alice's Novel", "alice-1", "", 1, "")
	h := &ProjectsHandler{repo: s}

	bob := &store.User{ID: 2, Role: "user", Username: "bob"}

	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodDelete, "/api/projects/1/", http.NoBody, bob)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob delete alice 应 403, 实际 %d", rec.Code)
	}

	// 验证项目未被删
	if _, err := s.Get(1); err != nil {
		t.Errorf("bob 越权 delete 成功, Get(1) err=%v", err)
	}
}

// TestProjectsHandler_Create_OwnerIDOverride 验证: 即使 request body 含 owner_id, 也用 context user.ID.
//
// P0-B 防越权: 恶意 user A 创建项目假装属于 user B (例如想栽赃).
func TestProjectsHandler_Create_OwnerIDOverride(t *testing.T) {
	s := NewProjectStore()
	h := &ProjectsHandler{repo: s}

	alice := &store.User{ID: 1, Role: "user", Username: "alice"}

	// alice POST 时, body 故意伪造 owner_id=999
	body, _ := json.Marshal(map[string]any{
		"name":     "Spoof",
		"slug":     "spoof-1",
		"owner_id": 999, // 想栽赃给 user 999
	})
	rec := httptest.NewRecorder()
	req := newRequestAsUser(http.MethodPost, "/api/projects", bytes.NewReader(body), alice)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}

	var p Project
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.OwnerID != alice.ID {
		t.Errorf("owner_id 应被忽略, 强制用 context user.ID=%d, 实际 OwnerID=%d",
			alice.ID, p.OwnerID)
	}
}

// TestProjectsHandler_Create_NoUser_Returns401 验证: create 无 user → 401.
func TestProjectsHandler_Create_NoUser_Returns401(t *testing.T) {
	h := NewProjectsHandler()
	body, _ := json.Marshal(map[string]string{"name": "X", "slug": "x"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("create 无 user 应 401, 实际 %d", rec.Code)
	}
}

// TestProjectsHandler_Update_NoUser_Returns401 验证: update 无 user → 401.
func TestProjectsHandler_Update_NoUser_Returns401(t *testing.T) {
	s := NewProjectStore()
	_, _ = s.Create("Pre", "pre", "", 1, "")
	h := &ProjectsHandler{repo: s}

	body, _ := json.Marshal(map[string]string{"name": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/projects/1/", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("update 无 user 应 401, 实际 %d", rec.Code)
	}
}

// TestProjectsHandler_Delete_NoUser_Returns401 验证: delete 无 user → 401.
func TestProjectsHandler_Delete_NoUser_Returns401(t *testing.T) {
	s := NewProjectStore()
	_, _ = s.Create("Pre", "pre", "", 1, "")
	h := &ProjectsHandler{repo: s}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/projects/1/", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("delete 无 user 应 401, 实际 %d", rec.Code)
	}
}

// =====================================================================
// Sprint V1.0.1 P0-A mux-level auth wire 集成测试 (用 full api.Router)
//
// 验证: 注册的 mux-level RequireAuth wrap 在端到端路径上生效.
// 直接 handler 调用 (上方 TestProjectsHandler_*) 不受影响 — 它们不走 mux.
// =====================================================================

// setupProjectsAuthMux 构造含 /api/auth/* + /api/projects/* 的完整 mux (用 api.Router).
//
// 最小依赖: SQLite + SessionManager + RateLimiter + ProjectsStore.
// 返回 mux 和一个 cleanup 函数 (test 用 t.Cleanup).
func setupProjectsAuthMux(t *testing.T) http.Handler {
	t.Helper()

	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sm := auth.NewSessionManager(db, auth.DefaultSessionConfig())
	limiter := auth.NewRateLimiter(5, 1*time.Minute, 30*time.Second)

	// 构造 Deps — 只填 Router 需要的最小字段 (Projects 只需 projectStore + session).
	deps := Deps{
		Store:   db,
		Session: sm,
		Limiter: limiter,
	}
	deps.SetProjectStore(NewSQLiteProjectsAdapter(store.NewProjectsStore(db)))

	mux := Router(deps)
	if mux == nil {
		t.Fatal("Router 返回 nil")
	}
	return mux
}

// TestProjectsMux_RequiresAuth_NoCookie_401 验证: /api/projects (mux-level) 无 cookie → 401.
//
// 直接证明 RequireAuth 在 mux.Handle wrap 中生效.
func TestProjectsMux_RequiresAuth_NoCookie_401(t *testing.T) {
	mux := setupProjectsAuthMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/", http.NoBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 cookie 应 401, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestProjectsMux_RequiresAuth_WithCookie_OK 验证: 带 cookie → 200 (requireauth 透明).
//
// 用 testfixtures.LoginAs 拿 cookie (复用 batch 1 的 helper).
func TestProjectsMux_RequiresAuth_WithCookie_OK(t *testing.T) {
	mux := setupProjectsAuthMux(t)
	cookie := testfixtures.LoginAs(t, mux, "alice_v101", "alicepass")

	resp := testfixtures.AuthedRequest(t, mux, http.MethodGet, "/api/projects/", nil, cookie)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("带 cookie 应 200, 实际 %d", resp.StatusCode)
	}
}

// TestProjectsMux_Create_RequiresAuth 验证: POST /api/projects 也需 cookie (不仅 GET).
//
// 注意: 此测试只验证 RequireAuth 在 POST 上的拦截效果 (401 vs 4xx),
// 不验证 owner_id 从 context 注入 (那是 P0-B 批次 2 范畴).
func TestProjectsMux_Create_RequiresAuth(t *testing.T) {
	mux := setupProjectsAuthMux(t)

	body, _ := json.Marshal(map[string]any{
		"name": "Test",
		// 故意用缺字段的 body — 通过 cookie 后会 400 (不是 401), 证明 auth 已通过
	})

	// 无 cookie → 401
	req := httptest.NewRequest(http.MethodPost, "/api/projects/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST 无 cookie 应 401, 实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 带 cookie → 4xx (但不是 401) → 证明 auth 通过, body 校验失败
	cookie := testfixtures.LoginAs(t, mux, "bob_v101", "bobpass")
	resp := testfixtures.AuthedRequest(t, mux, http.MethodPost, "/api/projects/", body, cookie)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("POST 带 cookie 不应 401 (auth 应通过), 实际 %d", resp.StatusCode)
	}
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Errorf("POST 带 cookie 应 4xx (body 校验失败), 实际 %d", resp.StatusCode)
	}
}

// TestProjectsMux_InvalidCookie_Returns401 验证: 伪造 cookie token → 401.
func TestProjectsMux_InvalidCookie_Returns401(t *testing.T) {
	mux := setupProjectsAuthMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "fake-token-xyz"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("伪造 cookie 应 401, 实际 %d", rec.Code)
	}
}
