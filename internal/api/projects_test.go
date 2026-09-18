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
	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
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

func TestProjectsHandler_CreateAndGet(t *testing.T) {
	h := NewProjectsHandler()

	body, _ := json.Marshal(map[string]string{
		"name":  "Test Novel",
		"slug":  "test-novel",
		"genre": "fantasy",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	var p Project
	_ = json.Unmarshal(rec.Body.Bytes(), &p)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/projects/1/", http.NoBody)
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
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/999/", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestProjectsHandler_UpdateAndDelete(t *testing.T) {
	h := NewProjectsHandler()

	// Create
	body, _ := json.Marshal(map[string]string{"name": "A", "slug": "a"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	var p Project
	_ = json.Unmarshal(rec.Body.Bytes(), &p)

	// Update
	body, _ = json.Marshal(map[string]string{"name": "A-updated", "genre": "scifi"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/projects/1/", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d", rec.Code)
	}
	var upd Project
	_ = json.Unmarshal(rec.Body.Bytes(), &upd)
	if upd.Name != "A-updated" || upd.Genre != "scifi" {
		t.Errorf("update failed: %+v", upd)
	}

	// Delete
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/projects/1/", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status=%d, want 204", rec.Code)
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
func TestProjectsMux_Create_RequiresAuth(t *testing.T) {
	mux := setupProjectsAuthMux(t)

	body, _ := json.Marshal(map[string]string{
		"name":        "Test",
		"slug":        "test-v101",
		"description": "Test project",
		"owner_id":    "0", // 实际从 context 注入, body 字段忽略
		"genre":       "fantasy",
	})

	// 无 cookie
	req := httptest.NewRequest(http.MethodPost, "/api/projects/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST 无 cookie 应 401, 实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 带 cookie
	cookie := testfixtures.LoginAs(t, mux, "bob_v101", "bobpass")
	resp := testfixtures.AuthedRequest(t, mux, http.MethodPost, "/api/projects/", body, cookie)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Errorf("POST 带 cookie 应 201, 实际 %d body=%s", resp.StatusCode, string(bodyBytes))
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
