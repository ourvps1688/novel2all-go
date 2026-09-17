package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/obs"
)

func newTestMetricsAdminHandler(t *testing.T) (*MetricsAdminHandler, *obs.Metrics) {
	t.Helper()
	metrics := obs.NewMetrics("test", "test", "test")
	session := newMockLookup("admin-token", "user-token")
	return NewMetricsAdminHandler(metrics, session), metrics
}

func TestMetricsAdmin_Reset_RequiresAuth(t *testing.T) {
	h, _ := newTestMetricsAdminHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/metrics/reset", http.NoBody)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rr.Code)
	}
}

func TestMetricsAdmin_Reset_RequiresAdmin(t *testing.T) {
	h, _ := newTestMetricsAdminHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/metrics/reset", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", rr.Code)
	}
}

func TestMetricsAdmin_Reset_AdminSuccess(t *testing.T) {
	h, metrics := newTestMetricsAdminHandler(t)

	// 增加一些 metrics
	metrics.IncHTTPRequests("GET", "/health", 200)
	metrics.IncHTTPRequests("POST", "/api/auth/login", 401)
	metrics.IncLLMCall("minimax", "WRITING", "success")
	metrics.AddLLMTokens("minimax", "prompt", 100)
	metrics.IncCacheHits()
	metrics.IncCacheMisses()
	metrics.IncSSEActive()

	// Reset via handler
	req := httptest.NewRequest(http.MethodPost, "/api/metrics/reset", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 验证 reset=true
	if reset, _ := resp["reset"].(bool); !reset {
		t.Error("expected reset=true")
	}

	// 验证 snapshot_before 有数据
	before, _ := resp["snapshot_before"].(map[string]any)
	if before == nil {
		t.Fatal("missing snapshot_before")
	}
	if httpTotal, _ := before["http_requests_total"].(float64); httpTotal < 2 {
		t.Errorf("expected http_requests_total >= 2, got %v", before["http_requests_total"])
	}

	// 验证 snapshot_after 都是 0
	after, _ := resp["snapshot_after"].(map[string]any)
	if after == nil {
		t.Fatal("missing snapshot_after")
	}
	if v, _ := after["http_requests_total"].(float64); v != 0 {
		t.Errorf("expected http_requests_total=0 after reset, got %v", v)
	}
	if v, _ := after["llm_calls_total"].(float64); v != 0 {
		t.Errorf("expected llm_calls_total=0 after reset, got %v", v)
	}
	if v, _ := after["cache_hits"].(float64); v != 0 {
		t.Errorf("expected cache_hits=0 after reset, got %v", v)
	}
	if v, _ := after["sse_active_streams"].(float64); v != 0 {
		t.Errorf("expected sse_active_streams=0 after reset, got %v", v)
	}

	// 验证实际 metrics 状态也清零（用 Snapshot 二次确认）
	snap := metrics.Snapshot()
	if len(snap.HTTPRequests) > 0 {
		for k, v := range snap.HTTPRequests {
			if v != 0 {
				t.Errorf("HTTPRequests[%s]=%d, expected 0", k, v)
			}
		}
	}
	if snap.CacheHitsTotal != 0 {
		t.Errorf("CacheHitsTotal=%d, expected 0", snap.CacheHitsTotal)
	}
}

func TestMetricsAdmin_MethodNotAllowed(t *testing.T) {
	h, _ := newTestMetricsAdminHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/reset", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", rr.Code)
	}
}

func TestMetricsAdmin_NilMetricsReturns503(t *testing.T) {
	session := newMockLookup("admin-token", "user-token")
	h := NewMetricsAdminHandler(nil, session)

	req := httptest.NewRequest(http.MethodPost, "/api/metrics/reset", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with nil metrics, got %d", rr.Code)
	}
}
