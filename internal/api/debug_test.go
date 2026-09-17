package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/obs"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// mockUserLookup 实现 UserLookup 接口（测试用）
type mockUserLookup struct {
	users map[string]*store.User // key: token
}

func (m *mockUserLookup) GetUserByToken(_ context.Context, token string) (*store.User, error) {
	if u, ok := m.users[token]; ok {
		return u, nil
	}
	return nil, auth.ErrUnauthorized
}

func newMockLookup(adminToken, userToken string) *mockUserLookup {
	now := time.Now()
	return &mockUserLookup{
		users: map[string]*store.User{
			adminToken: {
				ID:        1,
				Username:  "admin",
				Role:      "admin",
				Disabled:  false,
				CreatedAt: now,
			},
			userToken: {
				ID:        2,
				Username:  "alice",
				Role:      "user",
				Disabled:  false,
				CreatedAt: now,
			},
		},
	}
}

func newDebugHandler(lookup UserLookup, m *obs.Metrics, tr *obs.TraceRecorder) *DebugHandler {
	return NewDebugHandler(lookup, m, tr)
}

func TestDebugHandler_Traces_RequiresAuth(t *testing.T) {
	h := newDebugHandler(newMockLookup("admin-token", "user-token"), obs.NewMetrics("test", "test", "test"), obs.NewTraceRecorder(10))

	// 无 token → 401
	req := httptest.NewRequest(http.MethodGet, "/debug/traces", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}

	// 无效 token → 401
	req = httptest.NewRequest(http.MethodGet, "/debug/traces", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "bogus"})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDebugHandler_Traces_RequiresAdmin(t *testing.T) {
	lookup := newMockLookup("admin-token", "user-token")
	h := newDebugHandler(lookup, obs.NewMetrics("test", "test", "test"), obs.NewTraceRecorder(10))

	// user token → 403
	req := httptest.NewRequest(http.MethodGet, "/debug/traces", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin user, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDebugHandler_Traces_AdminSuccess(t *testing.T) {
	metrics := obs.NewMetrics("test", "test", "test")
	metrics.IncHTTPRequests("GET", "/health", 200)
	traces := obs.NewTraceRecorder(10)
	traces.RecordHTTP("GET", "/health", 200, 5*time.Millisecond)
	traces.RecordLLM("minimax", "WRITING", "success", 100*time.Millisecond, 50, 200)

	h := newDebugHandler(newMockLookup("admin-token", "user-token"), metrics, traces)

	req := httptest.NewRequest(http.MethodGet, "/debug/traces", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp TraceResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("expected 2 traces, got %d", resp.Count)
	}
	if resp.Capacity != 10 {
		t.Errorf("expected capacity=10, got %d", resp.Capacity)
	}
	// 最新在前
	if resp.Traces[0].Type != obs.TraceLLM {
		t.Errorf("expected first trace to be LLM (latest), got %q", resp.Traces[0].Type)
	}
	if resp.Traces[0].Provider != "minimax" {
		t.Errorf("expected provider=minimax, got %q", resp.Traces[0].Provider)
	}
	if resp.Traces[0].TokensIn != 50 || resp.Traces[0].TokensOut != 200 {
		t.Errorf("expected tokens 50/200, got %d/%d", resp.Traces[0].TokensIn, resp.Traces[0].TokensOut)
	}
}

func TestDebugHandler_Traces_TypeFilter(t *testing.T) {
	traces := obs.NewTraceRecorder(10)
	traces.RecordHTTP("GET", "/health", 200, time.Millisecond)
	traces.RecordHTTP("GET", "/api/auth/me", 200, time.Millisecond)
	traces.RecordLLM("minimax", "WRITING", "success", 100*time.Millisecond, 50, 200)

	h := newDebugHandler(newMockLookup("admin-token", "user-token"), obs.NewMetrics("test", "test", "test"), traces)

	req := httptest.NewRequest(http.MethodGet, "/debug/traces?type=http", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp TraceResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("expected 2 HTTP traces, got %d", resp.Count)
	}
	if resp.Filter != "http" {
		t.Errorf("expected filter=http, got %q", resp.Filter)
	}
	for _, tr := range resp.Traces {
		if tr.Type != obs.TraceHTTP {
			t.Errorf("filter leaked non-HTTP trace: %+v", tr)
		}
	}
}

func TestDebugHandler_Info_RequiresAdmin(t *testing.T) {
	h := newDebugHandler(newMockLookup("admin-token", "user-token"), obs.NewMetrics("test", "test", "test"), obs.NewTraceRecorder(10))

	req := httptest.NewRequest(http.MethodGet, "/debug/info", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rr.Code)
	}
}

func TestDebugHandler_Info_Structure(t *testing.T) {
	metrics := obs.NewMetrics("0.12.0", "abc1234", "1.22.12")
	metrics.IncHTTPRequests("GET", "/health", 200)
	metrics.IncLLMCall("minimax", "WRITING", "success")
	metrics.IncCacheHits()
	metrics.IncSSEActive()

	traces := obs.NewTraceRecorder(50)
	traces.RecordHTTP("GET", "/health", 200, time.Millisecond)

	h := newDebugHandler(newMockLookup("admin-token", "user-token"), metrics, traces)

	req := httptest.NewRequest(http.MethodGet, "/debug/info", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp InfoResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version.Version != "0.12.0" {
		t.Errorf("expected version 0.12.0, got %q", resp.Version.Version)
	}
	if resp.Runtime.SSEActive != 1 {
		t.Errorf("expected SSE active=1, got %d", resp.Runtime.SSEActive)
	}
	if resp.Metrics.HTTPTotal != 1 {
		t.Errorf("expected HTTP total=1, got %d", resp.Metrics.HTTPTotal)
	}
	if resp.Metrics.LLMCallsTotal != 1 {
		t.Errorf("expected LLM calls=1, got %d", resp.Metrics.LLMCallsTotal)
	}
	if resp.Metrics.CacheHits != 1 {
		t.Errorf("expected cache hits=1, got %d", resp.Metrics.CacheHits)
	}
	if resp.Traces.Size != 1 {
		t.Errorf("expected traces size=1, got %d", resp.Traces.Size)
	}
	if resp.Traces.Capacity != 50 {
		t.Errorf("expected traces capacity=50, got %d", resp.Traces.Capacity)
	}
	if resp.Timestamp.IsZero() {
		t.Error("expected Timestamp to be set")
	}
}

func TestDebugHandler_UnknownSubPath(t *testing.T) {
	h := newDebugHandler(newMockLookup("admin-token", "user-token"), obs.NewMetrics("test", "test", "test"), obs.NewTraceRecorder(10))

	req := httptest.NewRequest(http.MethodGet, "/debug/unknown", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown subpath, got %d", rr.Code)
	}
}

func TestDebugHandler_MethodNotAllowed(t *testing.T) {
	h := newDebugHandler(newMockLookup("admin-token", "user-token"), obs.NewMetrics("test", "test", "test"), obs.NewTraceRecorder(10))

	// POST /debug/traces → 405
	req := httptest.NewRequest(http.MethodPost, "/debug/traces", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST /debug/traces, got %d", rr.Code)
	}

	// POST /debug/info → 405
	req = httptest.NewRequest(http.MethodPost, "/debug/info", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST /debug/info, got %d", rr.Code)
	}
}

func TestDebugHandler_NilManagerReturns503(t *testing.T) {
	h := NewDebugHandler(nil, obs.NewMetrics("test", "test", "test"), obs.NewTraceRecorder(10))

	req := httptest.NewRequest(http.MethodGet, "/debug/traces", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with nil manager, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// 防止 unused 警告
var _ = errors.New
