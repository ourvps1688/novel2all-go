package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// TestHealthReady_Live 验证 /health/live 始终 200
func TestHealthReady_Live(t *testing.T) {
	// 用 nil db + nil router 也应该 200 (liveness 不检查依赖)
	h := NewHealthReadyHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp HealthCheck
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status=ok, got %q", resp.Status)
	}
	if resp.Checks == nil {
		t.Error("expected non-nil Checks map")
	}
	if len(resp.Checks) != 0 {
		t.Errorf("liveness should have empty checks, got %d", len(resp.Checks))
	}
}

// TestHealthReady_Ready_NilDeps 没有依赖时只返回基础响应
func TestHealthReady_Ready_NilDeps(t *testing.T) {
	h := NewHealthReadyHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with no deps, got %d", rr.Code)
	}

	var resp HealthCheck
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Status != "ok" {
		t.Errorf("expected status=ok with no deps, got %q", resp.Status)
	}
}

// TestHealthReady_Ready_DBOpen 用真实 DB 测试（open + migrate 后 ping 应该 ok）
func TestHealthReady_Ready_DBOpen(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(context.Background(), dir+"/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	router := llm.NewRouter(llm.Config{}) // no API keys → 0 providers
	h := NewHealthReadyHandler(db, router)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// DB ok + LLM fail → status=down, HTTP 503
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with no LLM providers, got %d", rr.Code)
	}

	var resp HealthCheck
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Status != "down" {
		t.Errorf("expected status=down, got %q", resp.Status)
	}
	if dbCheck, ok := resp.Checks["database"]; !ok {
		t.Error("missing database check")
	} else if dbCheck.Status != "ok" {
		t.Errorf("expected database ok, got %q", dbCheck.Status)
	}
	if llmCheck, ok := resp.Checks["llm"]; !ok {
		t.Error("missing llm check")
	} else if llmCheck.Status != "fail" {
		t.Errorf("expected llm fail (no providers), got %q", llmCheck.Status)
	}
}

// TestHealthReady_Ready_DBClosed DB 已关闭时 ping 应失败
func TestHealthReady_Ready_DBClosed(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(context.Background(), dir+"/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_ = db.Migrate(context.Background())
	_ = db.Close() // 立即关闭

	router := llm.NewRouter(llm.Config{DeepSeekAPIKey: "fake-key-for-test"})
	h := NewHealthReadyHandler(db, router)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with closed DB, got %d", rr.Code)
	}
	var resp HealthCheck
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Status != "down" {
		t.Errorf("expected status=down, got %q", resp.Status)
	}
	if dbCheck := resp.Checks["database"]; dbCheck.Status != "fail" {
		t.Errorf("expected database fail, got %q", dbCheck.Status)
	}
}

// TestHealthReady_Ready_LLMProviders 验证 provider 数量 > 0 时 LLM 检查 ok
func TestHealthReady_Ready_LLMProviders(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(context.Background(), dir+"/test.db")
	defer func() { _ = db.Close() }()
	_ = db.Migrate(context.Background())

	router := llm.NewRouter(llm.Config{
		DeepSeekAPIKey:  "fake-1",
		DashScopeAPIKey: "fake-2",
	})
	h := NewHealthReadyHandler(db, router)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with providers, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp HealthCheck
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if llmCheck := resp.Checks["llm"]; !strings.Contains(llmCheck.Detail, "deepseek") {
		t.Errorf("expected llm detail to mention deepseek, got %q", llmCheck.Detail)
	}
}

// TestHealthReady_UnknownPath 未知子路径 404
func TestHealthReady_UnknownPath(t *testing.T) {
	h := NewHealthReadyHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/health/unknown", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown subpath, got %d", rr.Code)
	}
}

// TestHealthReady_LiveUptimeNonZero 验证 uptime > 0
func TestHealthReady_LiveUptimeNonZero(t *testing.T) {
	h := NewHealthReadyHandler(nil, nil)
	// 等待 10ms
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp HealthCheck
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Uptime == "" {
		t.Error("expected non-empty uptime")
	}
}
