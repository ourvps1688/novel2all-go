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

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// newTestStateHandler 创建临时 state handler (Sprint V1.0.1 P3: 移除 session 参数).
//
// 返回 (*StateHandler, *ProjectStore). 鉴权已移到 mux-level middleware,
// 直接 handler 调用不走鉴权. 测试若需模拟 admin user context, 用 stateReqAdmin helper.
func newTestStateHandler(t *testing.T) (*StateHandler, *ProjectStore) {
	t.Helper()
	dir := t.TempDir()
	ps := NewProjectStore()
	p := NewStatePersistor(filepath.Join(dir, "state.json"), ps)
	return NewStateHandler(p), ps
}

// stateReqAdmin 构造带 admin user context 的 request (用于测试 admin-only handler).
//
// 注: handler 不再做 inline admin check; 测试需手动注入 admin user context.
func stateReqAdmin(method, url string, body []byte) *http.Request {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, url, bodyReader)
	admin := &store.User{ID: 999, Role: "admin", Username: "test-admin"}
	ctx := context.WithValue(req.Context(), userCtxValue, admin)
	return req.WithContext(ctx)
}

func TestStateHandler_Info_OK(t *testing.T) {
	h, _ := newTestStateHandler(t)

	req := stateReqAdmin(http.MethodGet, "/api/state", nil)
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
	h, store := newTestStateHandler(t)

	// 1. 创建 project
	if _, err := store.Create("My Novel", "novel-1", "test", 1, "fantasy"); err != nil {
		t.Fatalf("create: %v", err)
	}
	atomicStoreInt64(&cacheHits, 42)

	// 2. Save
	req := stateReqAdmin(http.MethodPost, "/api/state/save", nil)
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
	h2 := NewStateHandler(p2)

	req = stateReqAdmin(http.MethodPost, "/api/state/reload", nil)
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
	h, store := newTestStateHandler(t)

	// 1. 创建 project + save
	if _, err := store.Create("To Delete", "del", "", 1, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.persistor.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// 2. Reset via handler
	req := stateReqAdmin(http.MethodPost, "/api/state/reset", nil)
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
	h, _ := newTestStateHandler(t)

	// POST /api/state (without subpath) → 405
	req := stateReqAdmin(http.MethodPost, "/api/state", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST /api/state, got %d", rr.Code)
	}

	// GET /api/state/save → 405
	req = stateReqAdmin(http.MethodGet, "/api/state/save", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET /api/state/save, got %d", rr.Code)
	}
}

func TestStateHandler_UnknownSubPath(t *testing.T) {
	h, _ := newTestStateHandler(t)

	req := stateReqAdmin(http.MethodGet, "/api/state/unknown", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown subpath, got %d", rr.Code)
	}
}

func TestStateHandler_NilPersistorReturns503(t *testing.T) {
	// Sprint V1.0.1 P3: 移除 session 后, NewStateHandler 只接 persistor.
	// 仍保留 "nil persistor → 503" 测试 (验证 handler 防御性检查).
	h := NewStateHandler(nil)
	req := stateReqAdmin(http.MethodGet, "/api/state", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with nil persistor, got %d", rr.Code)
	}
}

// 防止 unused 警告
var _ = time.Now
