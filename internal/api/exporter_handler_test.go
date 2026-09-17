package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestExporterHandler(t *testing.T) *ExporterHandler {
	t.Helper()
	return NewExporterHandler(newMockLookup("admin-token", "user-token"))
}

func TestExporter_Formats(t *testing.T) {
	h := newTestExporterHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/exporter/formats", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Formats []map[string]string `json:"formats"`
		Count   int                 `json:"count"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 4 {
		t.Errorf("expected 4 formats, got %d", resp.Count)
	}
	// 检查 md
	hasMD := false
	hasPDF := false
	for _, f := range resp.Formats {
		if f["format"] == "md" {
			hasMD = true
		}
		if f["format"] == "pdf" {
			hasPDF = true
		}
	}
	if !hasMD || !hasPDF {
		t.Error("expected md and pdf in formats list")
	}
}

func TestExporter_Render_RequiresAuth(t *testing.T) {
	h := newTestExporterHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/exporter/render",
		strings.NewReader(`{"title":"x","body":"y","format":"md"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestExporter_Render_AdminSuccess(t *testing.T) {
	h := newTestExporterHandler(t)

	body := `{"title":"Test","body":"# Hello","format":"md"}`
	req := httptest.NewRequest(http.MethodPost, "/api/exporter/render", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("wrong Content-Type: %s", ct)
	}
	if !strings.Contains(rr.Body.String(), "Test") {
		t.Error("body should contain title")
	}
}

func TestExporter_Render_DefaultMD(t *testing.T) {
	h := newTestExporterHandler(t)

	// 不指定 format，应默认 md
	body := `{"title":"X","body":"Y"}`
	req := httptest.NewRequest(http.MethodPost, "/api/exporter/render", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("expected markdown mimetype, got %s", ct)
	}
}

func TestExporter_Render_InvalidJSON(t *testing.T) {
	h := newTestExporterHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/exporter/render", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestExporter_Chapter_RequiresAuth(t *testing.T) {
	h := newTestExporterHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/exporter/chapter/1?format=md", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestExporter_Chapter_NotFound(t *testing.T) {
	h := newTestExporterHandler(t)

	// 用不存在的 project_root，章节文件肯定没有
	req := httptest.NewRequest(http.MethodGet, "/api/exporter/chapter/1?format=md&project_root=Z:/nonexistent", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestExporter_Chapter_InvalidNumber(t *testing.T) {
	h := newTestExporterHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/exporter/chapter/abc", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestExporter_NilSessionReturns503(t *testing.T) {
	h := NewExporterHandler(nil)
	// nil session 应该对 admin-only 端点 (render, chapter) 返回 503
	// /formats 端点不要求 admin，可以正常返回
	req := httptest.NewRequest(http.MethodPost, "/api/exporter/render",
		strings.NewReader(`{"title":"x","body":"y"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil session render, got %d", rr.Code)
	}
}
