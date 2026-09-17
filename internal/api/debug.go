package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/obs"
	"github.com/ourvps1688/novel2all-go/internal/store"
	"github.com/ourvps1688/novel2all-go/internal/version"
)

// UserLookup 是 DebugHandler 依赖的最小接口（便于测试注入 mock）。
//
// *auth.SessionManager 自动实现该接口。
type UserLookup interface {
	// GetTokenFromRequest 从 HTTP 请求中提取 session token（cookie / header）。
	GetTokenFromRequest(r *http.Request) string
	// GetUserByToken 通过 token 查找 user。
	GetUserByToken(ctx context.Context, token string) (*store.User, error)
}

// DebugHandler 暴露 /debug/* 路由（admin only）。
//
// 支持的子路由：
//   GET /debug/traces?type=http|llm|error
//   GET /debug/info
type DebugHandler struct {
	manager UserLookup
	metrics *obs.Metrics
	traces  *obs.TraceRecorder
}

// NewDebugHandler 创建 DebugHandler。
func NewDebugHandler(m UserLookup, metrics *obs.Metrics, traces *obs.TraceRecorder) *DebugHandler {
	return &DebugHandler{
		manager: m,
		metrics: metrics,
		traces:  traces,
	}
}

// TraceResponse GET /debug/traces 响应。
type TraceResponse struct {
	Traces   []obs.Trace `json:"traces"`
	Count    int         `json:"count"`
	Capacity int         `json:"capacity"`
	Filter   string      `json:"filter,omitempty"`
}

// InfoResponse GET /debug/info 响应。
type InfoResponse struct {
	Version   version.Info   `json:"version"`
	Runtime   RuntimeInfo    `json:"runtime"`
	Metrics   MetricsSummary `json:"metrics"`
	Traces    TraceInfo      `json:"traces"`
	Timestamp time.Time      `json:"timestamp"`
}

// RuntimeInfo 运行时统计。
type RuntimeInfo struct {
	GoRoutines    int     `json:"goroutines"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	SSEActive     int64   `json:"sse_active_streams"`
}

// MetricsSummary metrics 概览（聚合 counts）。
type MetricsSummary struct {
	HTTPTotal        int   `json:"http_total"`
	LLMCallsTotal    int   `json:"llm_calls_total"`
	CacheHits        int64 `json:"cache_hits"`
	CacheMisses      int64 `json:"cache_misses"`
	DBSessionsActive int64 `json:"db_sessions_active"`
}

// TraceInfo trace buffer 状态。
type TraceInfo struct {
	Size     int `json:"size"`
	Capacity int `json:"capacity"`
}

// ServeHTTP 路由分发（admin 鉴权 + 子路由解析）。
func (h *DebugHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// admin 鉴权
	if h.manager == nil {
		http.Error(w, `{"error":"debug endpoints disabled"}`, http.StatusServiceUnavailable)
		return
	}
	token := h.manager.GetTokenFromRequest(r)
	user, err := h.manager.GetUserByToken(r.Context(), token)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if !auth.IsAdmin(user) {
		http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/debug")
	path = strings.Trim(path, "/")

	switch path {
	case "traces":
		h.handleTraces(w, r)
	case "info":
		h.handleInfo(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *DebugHandler) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	typeFilter := obs.TraceType(r.URL.Query().Get("type"))

	var traces []obs.Trace
	capacity := 0
	if h.traces != nil {
		traces = h.traces.Snapshot(typeFilter)
		capacity = h.traces.Capacity()
	} else {
		traces = []obs.Trace{}
	}

	resp := TraceResponse{
		Traces:   traces,
		Count:    len(traces),
		Capacity: capacity,
		Filter:   string(typeFilter),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *DebugHandler) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.metrics == nil {
		http.Error(w, `{"error":"metrics not enabled"}`, http.StatusServiceUnavailable)
		return
	}

	snap := h.metrics.Snapshot()

	httpTotal := 0
	for _, v := range snap.HTTPRequests {
		httpTotal += int(v)
	}
	llmTotal := 0
	for _, v := range snap.LLMCalls {
		llmTotal += int(v)
	}

	traceSize := 0
	traceCap := 0
	if h.traces != nil {
		traceSize = h.traces.Size()
		traceCap = h.traces.Capacity()
	}

	resp := InfoResponse{
		Version: version.Info{
			Version:   snap.Version,
			Commit:    snap.Commit,
			GoVersion: snap.GoVersion,
		},
		Runtime: RuntimeInfo{
			GoRoutines:    snap.GoRoutines,
			UptimeSeconds: snap.UptimeSeconds,
			SSEActive:     snap.SSEActiveStreams,
		},
		Metrics: MetricsSummary{
			HTTPTotal:        httpTotal,
			LLMCallsTotal:    llmTotal,
			CacheHits:        snap.CacheHitsTotal,
			CacheMisses:      snap.CacheMissesTotal,
			DBSessionsActive: snap.DBSessionsActive,
		},
		Traces: TraceInfo{
			Size:     traceSize,
			Capacity: traceCap,
		},
		Timestamp: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}
