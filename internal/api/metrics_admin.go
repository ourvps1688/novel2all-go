package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/obs"
)

// MetricsAdminHandler 提供 /api/metrics/* admin 端点
//
// 路由：
//
//	POST /api/metrics/reset    → 重置所有 metrics counters
//
// 注意：GET /api/metrics（Prometheus text）和 /debug/info 是 metrics 观测，
// 不需要 admin。本 handler 只负责 admin 操作（reset）。
type MetricsAdminHandler struct {
	metrics *obs.Metrics
	session UserLookup
}

// NewMetricsAdminHandler 创建
func NewMetricsAdminHandler(m *obs.Metrics, s UserLookup) *MetricsAdminHandler {
	return &MetricsAdminHandler{metrics: m, session: s}
}

// ServeHTTP 路由分发 + admin 鉴权
func (h *MetricsAdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// admin 鉴权
	if h.session == nil {
		http.Error(w, `{"error":"metrics admin endpoints disabled"}`, http.StatusServiceUnavailable)
		return
	}
	token := h.session.GetTokenFromRequest(r)
	user, err := h.session.GetUserByToken(r.Context(), token)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if !auth.IsAdmin(user) {
		http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.handleReset(w, r)
}

// handleReset POST /api/metrics/reset
//
// 重置所有 metrics counters 到零（HTTP / LLM / SSE / cache hits/misses / db sessions）。
// 注意：trace buffer 不重置（trace 是 ring buffer，新事件会覆盖旧事件）。
//
// 返回：
//   - reset: true
//   - snapshot_before: 重置前的关键指标快照（用于审计）
func (h *MetricsAdminHandler) handleReset(w http.ResponseWriter, _ *http.Request) {
	if h.metrics == nil {
		http.Error(w, `{"error":"metrics not enabled"}`, http.StatusServiceUnavailable)
		return
	}

	// 抓快照（重置前）
	before := h.metrics.Snapshot()

	h.metrics.Reset()

	// 抓重置后快照（应该都是 0）
	after := h.metrics.Snapshot()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"reset": true,
		"snapshot_before": map[string]any{
			"http_requests_total": sumMap(before.HTTPRequests),
			"llm_calls_total":     sumMap(before.LLMCalls),
			"sse_active_streams":  before.SSEActiveStreams,
			"cache_hits":          before.CacheHitsTotal,
			"cache_misses":        before.CacheMissesTotal,
			"db_sessions_active":  before.DBSessionsActive,
		},
		"snapshot_after": map[string]any{
			"http_requests_total": sumMap(after.HTTPRequests),
			"llm_calls_total":     sumMap(after.LLMCalls),
			"sse_active_streams":  after.SSEActiveStreams,
			"cache_hits":          after.CacheHitsTotal,
			"cache_misses":        after.CacheMissesTotal,
			"db_sessions_active":  after.DBSessionsActive,
		},
	})
}

// sumMap 求 map[string]int64 所有 value 之和（用于聚合 HTTPRequests/LLMCalls）
func sumMap(m map[string]int64) int64 {
	var total int64
	for _, v := range m {
		total += v
	}
	return total
}

// 防止 unused 警告
var _ = errors.New
