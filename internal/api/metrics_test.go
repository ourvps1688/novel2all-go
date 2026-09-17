package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/obs"
)

func TestMetricsHandler_ServesPrometheusText(t *testing.T) {
	m := obs.NewMetrics("0.12.0", "abc1234", "1.22.12")
	m.IncHTTPRequests("GET", "/health", 200)
	m.IncHTTPRequests("GET", "/health", 200)
	m.IncLLMCall("minimax", "WRITING", "success")
	m.AddLLMTokens("minimax", "prompt", 100)
	m.IncCacheHits()
	m.IncSSEActive()

	h := NewMetricsHandler(m)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected Content-Type text/plain, got %q", ct)
	}
	if !strings.Contains(ct, "version=0.0.4") {
		t.Errorf("expected Content-Type to declare version=0.0.4, got %q", ct)
	}

	body := rr.Body.String()
	required := []string{
		"novel2all_http_requests_total{method=\"GET\",path=\"/health\",status=\"200\"} 2",
		"novel2all_llm_calls_total{provider=\"minimax\",task=\"WRITING\",status=\"success\"} 1",
		"novel2all_llm_tokens_total{provider=\"minimax\",kind=\"prompt\"} 100",
		"novel2all_sse_active_streams 1",
		"novel2all_cache_hits_total 1",
		"novel2all_build_info{version=\"0.12.0\",commit=\"abc1234\",go=\"1.22.12\"} 1",
	}
	for _, sub := range required {
		if !strings.Contains(body, sub) {
			t.Errorf("missing required metric line: %q\n--- actual ---\n%s", sub, body)
		}
	}
}

func TestMetricsHandler_NilMetricsReturns503(t *testing.T) {
	h := NewMetricsHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with nil metrics, got %d", rr.Code)
	}
}

func TestMetricsHandler_NoAuthRequired(t *testing.T) {
	// 验证不要求 cookie / Authorization header
	h := NewMetricsHandler(obs.NewMetrics("test", "test", "test"))

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	// 故意不设置任何 cookie
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 without auth, got %d", rr.Code)
	}
}

func TestMetricsHandler_ObservesHTTPDuration(t *testing.T) {
	m := obs.NewMetrics("test", "test", "test")
	m.ObserveHTTPDuration("GET", "/api/cache/stats", 250*time.Millisecond)
	m.ObserveHTTPDuration("GET", "/api/cache/stats", 150*time.Millisecond)

	h := NewMetricsHandler(m)
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "novel2all_http_request_duration_seconds_sum{method=\"GET\",path=\"/api/cache/stats\"} 0.400") {
		t.Errorf("expected duration sum 0.400s (400ms), body:\n%s", body)
	}
	if !strings.Contains(body, "novel2all_http_request_duration_seconds_count{method=\"GET\",path=\"/api/cache/stats\"} 2") {
		t.Errorf("expected duration count 2, body:\n%s", body)
	}
}
