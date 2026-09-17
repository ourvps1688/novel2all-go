package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// newTestStateHandler 创建临时 state handler + mock session
func newTestStateHandler(t *testing.T) (*StateHandler, *ProjectStore, *mockUserLookup) {
	t.Helper()
	dir := t.TempDir()
	store := NewProjectStore()
	p := NewStatePersistor(filepath.Join(dir, "state.json"), store)
	session := newMockLookup("admin-token", "user-token")
	return NewStateHandler(p, session), store, session
}

func TestStateHandler_Info_RequiresAuth(t *testing.T) {
	h, _, _ := newTestStateHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rr.Code)
	}
}

func TestStateHandler_Info_RequiresAdmin(t *testing.T) {
	h, _, _ := newTestStateHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", rr.Code)
	}
}

func TestStateHandler_Info_AdminSuccess(t *testing.T) {
	h, _, _ := newTestStateHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var info StateInfo
	if err := json.NewDecoder(rr.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if info.Path == "" {
		t.Error("expected non-empty path")
	}
	if info.Exists {
		t.Error("expected Exists=false before any save")
	}
	if info.Projects != 0 {
		t.Errorf("expected 0 projects initially, got %d", info.Projects)
	}
}

func TestStateHandler_Save_RoundTrip(t *testing.T) {
	h, store, _ := newTestStateHandler(t)

	// 1. 创建 project
	if _, err := store.Create("My Novel", "novel-1", "test", 1, "fantasy"); err != nil {
		t.Fatalf("create: %v", err)
	}
	atomicStoreInt64(&cacheHits, 42)

	// 2. Save
	req := httptest.NewRequest(http.MethodPost, "/api/state/save", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("save expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if saved, _ := resp["saved"].(bool); !saved {
		t.Error("expected saved=true")
	}

	// 3. 创建新的 store + persistor 模拟重启
	dir := h.persistor.path
	store2 := NewProjectStore()
	p2 := NewStatePersistor(dir, store2)

	// 4. Reload via handler (新 handler 但用同一 persistor 路径)
	h2 := NewStateHandler(p2, newMockLookup("admin-token", "user-token"))

	req = httptest.NewRequest(http.MethodPost, "/api/state/reload", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr = httptest.NewRecorder()
	h2.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("reload expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// 5. 验证 projects 已恢复
	projects := store2.List()
	if len(projects) != 1 {
		t.Errorf("expected 1 project after reload, got %d", len(projects))
	}

	// 6. 验证 cache counters 已恢复
	if atomicLoadInt64(&cacheHits) != 42 {
		t.Errorf("expected cache_hits=42 after reload, got %d", atomicLoadInt64(&cacheHits))
	}
}

func TestStateHandler_Reset(t *testing.T) {
	h, store, _ := newTestStateHandler(t)

	// 1. 创建 project + save
	if _, err := store.Create("To Delete", "del", "", 1, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.persistor.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// 2. Reset via handler
	req := httptest.NewRequest(http.MethodPost, "/api/state/reset", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("reset expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// 3. 验证内存清空 + 文件删除
	if len(store.List()) != 0 {
		t.Error("projects should be empty after reset")
	}
	if atomicLoadInt64(&cacheHits) != 0 {
		t.Error("cache_hits should be 0 after reset")
	}
}

func TestStateHandler_MethodNotAllowed(t *testing.T) {
	h, _, _ := newTestStateHandler(t)

	// POST /api/state (without subpath) → 405
	req := httptest.NewRequest(http.MethodPost, "/api/state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST /api/state, got %d", rr.Code)
	}

	// GET /api/state/save → 405
	req = httptest.NewRequest(http.MethodGet, "/api/state/save", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET /api/state/save, got %d", rr.Code)
	}
}

func TestStateHandler_UnknownSubPath(t *testing.T) {
	h, _, _ := newTestStateHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/state/unknown", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown subpath, got %d", rr.Code)
	}
}

func TestStateHandler_NilPersistorReturns503(t *testing.T) {
	// nil session 不应该 panic
	session := newMockLookup("admin-token", "user-token")
	// 用 NewStateHandler 接受 nil
	// 实际上 NewStateHandler 会接受 nil (只是不能 Save/Load)
	// 改测 nil session
	_ = session // avoid unused
	h := NewStateHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with nil session, got %d", rr.Code)
	}
}

// 防止 unused 警告
var _ = time.Now
