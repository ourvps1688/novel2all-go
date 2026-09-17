// memory_test.go 测试 MemoryHandler HTTP 端点.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestMemoryHandler(t *testing.T) (*MemoryHandler, func()) {
	dir := t.TempDir()
	cleanup := func() { _ = dir }
	return NewMemoryHandler(dir), cleanup
}

func TestMemoryHandler_GetState(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/memory/state", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp MemoryStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryHandler_GetContext_InvalidChapter(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/memory/context/abc", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid chapter should be 400, got %d", rec.Code)
	}
}

func TestMemoryHandler_GetContext(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/memory/context/5", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestMemoryHandler_Update(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	body := strings.NewReader(`{"chapter":1,"content":"林雷走在苍茫镇。"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/update", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestMemoryHandler_Update_BadJSON(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/update", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad json should be 400, got %d", rec.Code)
	}
}

func TestMemoryHandler_Review(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	body := strings.NewReader(`{"chapter":1,"content":"测试内容"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/review", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var report map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &report)
	if _, ok := report["scores"]; !ok {
		t.Error("report missing scores")
	}
}

func TestMemoryHandler_ListSnapshots_Empty(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/memory/snapshots", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestMemoryHandler_Rollback_NotFound(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	body := strings.NewReader(`{"chapter":999}`)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/rollback", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("not-found should be 404, got %d", rec.Code)
	}
}

func TestMemoryHandler_NotFound(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/memory/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestMemoryHandler_MethodNotAllowed(t *testing.T) {
	h, _ := newTestMemoryHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/state", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /state should be 405, got %d", rec.Code)
	}
}
