package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/chroma"
)

func newTestChromaHandler(t *testing.T) *ChromaHandler {
	t.Helper()
	return NewChromaHandler(chroma.NewClient(64))
}

func TestChroma_Upsert_And_Search(t *testing.T) {
	h := newTestChromaHandler(t)

	// Upsert
	body := `{"documents":[{"text":"the quick brown fox"},{"text":"completely different text about cats"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chroma/upsert", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Search
	body = `{"text":"quick brown fox","k":1}`
	req = httptest.NewRequest(http.MethodPost, "/api/chroma/search", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("search: expected 200, got %d", rr.Code)
	}
	var resp struct {
		Matches []chroma.Match `json:"matches"`
		Count   int            `json:"count"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 {
		t.Errorf("expected 1 match, got %d", resp.Count)
	}
	if len(resp.Matches) > 0 && resp.Matches[0].Text != "the quick brown fox" {
		t.Errorf("expected top match 'the quick brown fox', got %q", resp.Matches[0].Text)
	}
}

func TestChroma_Stats(t *testing.T) {
	h := newTestChromaHandler(t)

	// 初始 stats
	req := httptest.NewRequest(http.MethodGet, "/api/chroma/stats", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var stats chroma.Stats
	_ = json.NewDecoder(rr.Body).Decode(&stats)
	if stats.DocumentCount != 0 || stats.VectorDim != 64 {
		t.Errorf("initial stats wrong: %+v", stats)
	}

	// Upsert 后
	body := `{"documents":[{"text":"a"},{"text":"b"},{"text":"c"}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/chroma/upsert", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	req = httptest.NewRequest(http.MethodGet, "/api/chroma/stats", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	_ = json.NewDecoder(rr.Body).Decode(&stats)
	if stats.DocumentCount != 3 {
		t.Errorf("expected 3 docs, got %d", stats.DocumentCount)
	}
}

func TestChroma_Get(t *testing.T) {
	h := newTestChromaHandler(t)

	// Upsert with explicit id
	body := `{"documents":[{"id":"my-doc","text":"hello","metadata":{"src":"test"}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chroma/upsert", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// Get
	req = httptest.NewRequest(http.MethodGet, "/api/chroma/my-doc", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var d chroma.Document
	_ = json.NewDecoder(rr.Body).Decode(&d)
	if d.Text != "hello" || d.Metadata["src"] != "test" {
		t.Errorf("doc mismatch: %+v", d)
	}

	// Get nonexistent
	req = httptest.NewRequest(http.MethodGet, "/api/chroma/nonexistent", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent, got %d", rr.Code)
	}
}

func TestChroma_Delete(t *testing.T) {
	h := newTestChromaHandler(t)

	// Setup
	body := `{"documents":[{"id":"del-me","text":"x"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chroma/upsert", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/chroma/del-me", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}

	// Get → 404
	req = httptest.NewRequest(http.MethodGet, "/api/chroma/del-me", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", rr.Code)
	}

	// Delete again → 404
	req = httptest.NewRequest(http.MethodDelete, "/api/chroma/del-me", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for double-delete, got %d", rr.Code)
	}
}

func TestChroma_Clear(t *testing.T) {
	h := newTestChromaHandler(t)

	// Add 3 docs
	body := `{"documents":[{"text":"a"},{"text":"b"},{"text":"c"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chroma/upsert", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// Clear
	req = httptest.NewRequest(http.MethodPost, "/api/chroma/clear", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// Verify empty
	req = httptest.NewRequest(http.MethodGet, "/api/chroma/stats", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var stats chroma.Stats
	_ = json.NewDecoder(rr.Body).Decode(&stats)
	if stats.DocumentCount != 0 {
		t.Errorf("expected 0 after clear, got %d", stats.DocumentCount)
	}
}

func TestChroma_MethodNotAllowed(t *testing.T) {
	h := newTestChromaHandler(t)

	// GET /api/chroma/upsert → 405
	req := httptest.NewRequest(http.MethodGet, "/api/chroma/upsert", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET upsert, got %d", rr.Code)
	}

	// POST /api/chroma/stats → 405
	req = httptest.NewRequest(http.MethodPost, "/api/chroma/stats", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST stats, got %d", rr.Code)
	}
}

func TestChroma_Upsert_InvalidJSON(t *testing.T) {
	h := newTestChromaHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/chroma/upsert", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid json, got %d", rr.Code)
	}
}

func TestChroma_NotFoundOnBasePath(t *testing.T) {
	h := newTestChromaHandler(t)

	// /api/chroma (no subpath) → 404
	req := httptest.NewRequest(http.MethodGet, "/api/chroma", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for base path, got %d", rr.Code)
	}
}
