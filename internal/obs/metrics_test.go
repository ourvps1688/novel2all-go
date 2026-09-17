package obs

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMetrics_NewMetrics_InitializesBuildInfo(t *testing.T) {
	m := NewMetrics("0.12.0", "abc1234", "1.22.12")
	snap := m.Snapshot()
	if snap.Version != "0.12.0" {
		t.Errorf("expected version 0.12.0, got %q", snap.Version)
	}
	if snap.Commit != "abc1234" {
		t.Errorf("expected commit abc1234, got %q", snap.Commit)
	}
	if snap.GoVersion != "1.22.12" {
		t.Errorf("expected go 1.22.12, got %q", snap.GoVersion)
	}
}

func TestMetrics_IncHTTPRequests(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	m.IncHTTPRequests("GET", "/health", 200)
	m.IncHTTPRequests("GET", "/health", 200)
	m.IncHTTPRequests("POST", "/api/auth/login", 401)

	snap := m.Snapshot()
	if snap.HTTPRequests["GET|/health|200"] != 2 {
		t.Errorf("expected GET /health 200 count=2, got %d", snap.HTTPRequests["GET|/health|200"])
	}
	if snap.HTTPRequests["POST|/api/auth/login|401"] != 1 {
		t.Errorf("expected POST /api/auth/login 401 count=1, got %d", snap.HTTPRequests["POST|/api/auth/login|401"])
	}
}

func TestMetrics_ObserveHTTPDuration(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	m.ObserveHTTPDuration("GET", "/health", 100*time.Millisecond)
	m.ObserveHTTPDuration("GET", "/health", 200*time.Millisecond)

	snap := m.Snapshot()
	if snap.HTTPDurationSum["GET|/health"] != 300 {
		t.Errorf("expected duration sum=300ms, got %d", snap.HTTPDurationSum["GET|/health"])
	}
	if snap.HTTPDurationCount["GET|/health"] != 2 {
		t.Errorf("expected duration count=2, got %d", snap.HTTPDurationCount["GET|/health"])
	}
}

func TestMetrics_IncLLMCall(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	m.IncLLMCall("minimax", "WRITING", "success")
	m.IncLLMCall("minimax", "WRITING", "success")
	m.IncLLMCall("deepseek", "CONSISTENCY", "error")

	snap := m.Snapshot()
	if snap.LLMCalls["minimax|WRITING|success"] != 2 {
		t.Errorf("expected 2 minimax WRITING success, got %d", snap.LLMCalls["minimax|WRITING|success"])
	}
	if snap.LLMCalls["deepseek|CONSISTENCY|error"] != 1 {
		t.Errorf("expected 1 deepseek CONSISTENCY error, got %d", snap.LLMCalls["deepseek|CONSISTENCY|error"])
	}
}

func TestMetrics_AddLLMTokens_IgnoresZeroAndNegative(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	m.AddLLMTokens("minimax", "prompt", 100)
	m.AddLLMTokens("minimax", "prompt", 0)
	m.AddLLMTokens("minimax", "prompt", -50)
	m.AddLLMTokens("minimax", "completion", 200)

	snap := m.Snapshot()
	if snap.LLMTokens["minimax|prompt"] != 100 {
		t.Errorf("expected prompt tokens=100 (zero/negative ignored), got %d", snap.LLMTokens["minimax|prompt"])
	}
	if snap.LLMTokens["minimax|completion"] != 200 {
		t.Errorf("expected completion tokens=200, got %d", snap.LLMTokens["minimax|completion"])
	}
}

func TestMetrics_Gauges(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	m.IncSSEActive()
	m.IncSSEActive()
	m.IncSSEActive()
	m.DecSSEActive()
	m.IncCacheHits()
	m.IncCacheMisses()
	m.IncCacheMisses()
	m.IncDBSessions()
	m.IncDBSessions()
	m.DecDBSessions()

	snap := m.Snapshot()
	if snap.SSEActiveStreams != 2 {
		t.Errorf("expected SSE active=2, got %d", snap.SSEActiveStreams)
	}
	if snap.CacheHitsTotal != 1 {
		t.Errorf("expected cache hits=1, got %d", snap.CacheHitsTotal)
	}
	if snap.CacheMissesTotal != 2 {
		t.Errorf("expected cache misses=2, got %d", snap.CacheMissesTotal)
	}
	if snap.DBSessionsActive != 1 {
		t.Errorf("expected DB sessions=1, got %d", snap.DBSessionsActive)
	}
}

func TestMetrics_PrometheusText_ContainsAllRequiredMetrics(t *testing.T) {
	m := NewMetrics("0.12.0", "abc1234", "1.22.12")
	m.IncHTTPRequests("GET", "/health", 200)
	m.ObserveHTTPDuration("GET", "/health", 50*time.Millisecond)
	m.IncLLMCall("minimax", "WRITING", "success")
	m.AddLLMTokens("minimax", "prompt", 100)
	m.IncCacheHits()

	text := m.PrometheusText()

	required := []string{
		"# HELP novel2all_http_requests_total",
		"# TYPE novel2all_http_requests_total counter",
		"novel2all_http_requests_total{method=\"GET\",path=\"/health\",status=\"200\"} 1",
		"# HELP novel2all_http_request_duration_seconds",
		"# TYPE novel2all_http_request_duration_seconds counter",
		"novel2all_http_request_duration_seconds_sum{method=\"GET\",path=\"/health\"}",
		"novel2all_http_request_duration_seconds_count{method=\"GET\",path=\"/health\"} 1",
		"# HELP novel2all_llm_calls_total",
		"novel2all_llm_calls_total{provider=\"minimax\",task=\"WRITING\",status=\"success\"} 1",
		"novel2all_llm_tokens_total{provider=\"minimax\",kind=\"prompt\"} 100",
		"novel2all_sse_active_streams 0",
		"novel2all_cache_hits_total 1",
		"novel2all_cache_misses_total 0",
		"novel2all_db_sessions_active 0",
		"novel2all_process_uptime_seconds",
		"novel2all_go_goroutines",
		"novel2all_build_info{version=\"0.12.0\",commit=\"abc1234\",go=\"1.22.12\"} 1",
	}
	for _, sub := range required {
		if !strings.Contains(text, sub) {
			t.Errorf("PrometheusText() missing required substring: %q\n--- actual ---\n%s", sub, text)
		}
	}
}

func TestMetrics_PrometheusText_DeterministicOrder(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	// 按非字母序插入
	m.IncHTTPRequests("POST", "/api/auth/login", 401)
	m.IncHTTPRequests("GET", "/api/skills/", 200)
	m.IncHTTPRequests("DELETE", "/api/auth/users/1", 204)

	text := m.PrometheusText()

	// 验证排序：DELETE 在 GET 之前（字母序）
	idxDelete := strings.Index(text, "DELETE")
	idxGet := strings.Index(text, "GET")
	idxPost := strings.Index(text, "POST")
	if idxDelete == -1 || idxGet == -1 || idxPost == -1 {
		t.Fatalf("missing expected HTTP request entries:\n%s", text)
	}
	if !(idxDelete < idxGet && idxGet < idxPost) {
		t.Errorf("expected sorted order DELETE < GET < POST, got indices %d < %d < %d:\n%s",
			idxDelete, idxGet, idxPost, text)
	}
}

func TestMetrics_ConcurrentIncrements(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				m.IncHTTPRequests("GET", "/health", 200)
				m.IncSSEActive()
				m.IncSSEActive()
				m.DecSSEActive()
				m.IncCacheHits()
			}
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	expected := int64(goroutines * iterations)
	if snap.HTTPRequests["GET|/health|200"] != expected {
		t.Errorf("expected HTTP count=%d, got %d", expected, snap.HTTPRequests["GET|/health|200"])
	}
	if snap.SSEActiveStreams != expected {
		t.Errorf("expected SSE active=%d, got %d", expected, snap.SSEActiveStreams)
	}
	if snap.CacheHitsTotal != expected {
		t.Errorf("expected cache hits=%d, got %d", expected, snap.CacheHitsTotal)
	}
}

func TestMetrics_SnapshotIsImmutable(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	m.IncHTTPRequests("GET", "/health", 200)

	snap1 := m.Snapshot()
	m.IncHTTPRequests("GET", "/health", 200)
	snap2 := m.Snapshot()

	if snap1.HTTPRequests["GET|/health|200"] != 1 {
		t.Errorf("snap1 should not see later increments, got %d", snap1.HTTPRequests["GET|/health|200"])
	}
	if snap2.HTTPRequests["GET|/health|200"] != 2 {
		t.Errorf("snap2 should see latest, got %d", snap2.HTTPRequests["GET|/health|200"])
	}
}

func TestMetrics_UptimeProgresses(t *testing.T) {
	m := NewMetrics("test", "test", "test")
	snap1 := m.Snapshot()
	time.Sleep(10 * time.Millisecond)
	snap2 := m.Snapshot()

	if snap2.UptimeSeconds <= snap1.UptimeSeconds {
		t.Errorf("expected uptime to progress: snap1=%f, snap2=%f", snap1.UptimeSeconds, snap2.UptimeSeconds)
	}
}
