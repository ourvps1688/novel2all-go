// health_ready.go 提供深度健康检查（readiness probe + liveness probe）。
//
// 设计目标：
//   - /health/live   → 进程是否还活着（用于 k8s liveness probe）
//   - /health/ready  → 依赖（DB + LLM router）是否健康（用于 k8s readiness probe）
//   - /health        → 保留向后兼容（基础存活）
//
// 响应格式：
//
//	{
//	  "status":    "ok" | "degraded" | "down",
//	  "timestamp": "2026-09-17T...",
//	  "uptime":    "1h23m",
//	  "checks": {
//	    "database": { "status": "ok", "detail": "data/novel2all.db" },
//	    "llm":      { "status": "ok", "detail": "4 providers available" }
//	  }
//	}
//
// HTTP 状态码：
//   - /health/live  → always 200 (只要进程响应)
//   - /health/ready → 200 if all checks ok, 503 if any fail
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// HealthCheck 健康检查响应
type HealthCheck struct {
	Status    string           `json:"status"` // ok | degraded | down
	Timestamp string           `json:"timestamp"`
	Uptime    string           `json:"uptime"`
	Checks    map[string]Check `json:"checks"`
}

// Check 单个依赖检查结果
type Check struct {
	Status string `json:"status"` // ok | fail
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// readinessTimeout 健康检查超时（避免 probe hang）
const readinessTimeout = 2 * time.Second

// Check status 常量（goconst: 避免硬编码字符串重复）
const (
	checkStatusOK   = "ok"
	checkStatusFail = "fail"
)

// Health status 常量（goconst: 避免硬编码字符串重复）
const (
	healthStatusOK   = "ok"
	healthStatusDown = "down"
)

// HealthReadyHandler 提供 /health/ready + /health/live
//
// 注入 DB 和 LLMRouter 用于依赖检查。
// 不注入则只返回基础存活（向后兼容）。
type HealthReadyHandler struct {
	startTime time.Time
	db        *store.DB
	router    *llm.Router
}

// NewHealthReadyHandler 创建
func NewHealthReadyHandler(db *store.DB, router *llm.Router) *HealthReadyHandler {
	return &HealthReadyHandler{
		startTime: time.Now(),
		db:        db,
		router:    router,
	}
}

// ServeHTTP 路由分发（基于 path 子段）
func (h *HealthReadyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// /health/live → liveness
	// /health/ready → readiness
	// 其他 → 405
	path := strings.TrimPrefix(r.URL.Path, "/health/")
	path = strings.Trim(path, "/")

	switch path {
	case "live":
		h.handleLive(w, r)
	case "ready":
		h.handleReady(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleLive liveness probe — 进程是否响应
//
// always 200（除非 panic 或 hang）。
// 用于 k8s liveness probe：如果挂了就重启 pod。
func (h *HealthReadyHandler) handleLive(w http.ResponseWriter, _ *http.Request) {
	resp := HealthCheck{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    time.Since(h.startTime).String(),
		Checks:    map[string]Check{},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleReady readiness probe — 依赖是否健康
//
// 检查 DB Ping + LLM providers 数量。
// HTTP 200 = 全部 ok, 503 = 有依赖 fail。
// 用于 k8s readiness probe：如果依赖挂了就不接流量。
func (h *HealthReadyHandler) handleReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]Check{}

	// 1. Database check
	if h.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		if err := h.db.PingContext(ctx); err != nil {
			checks["database"] = Check{
				Status: checkStatusFail,
				Error:  err.Error(),
			}
		} else {
			checks["database"] = Check{
				Status: checkStatusOK,
				Detail: "ping succeeded",
			}
		}
	}

	// 2. LLM router check
	if h.router != nil {
		providers := h.router.AvailableProviders()
		if len(providers) == 0 {
			checks["llm"] = Check{
				Status: checkStatusFail,
				Error:  "no LLM providers available (check API keys)",
			}
		} else {
			checks["llm"] = Check{
				Status: checkStatusOK,
				Detail: formatProviders(providers),
			}
		}
	}

	// 聚合状态：所有 ok → ok, 任意 fail → down
	status := healthStatusOK
	httpCode := http.StatusOK
	for _, c := range checks {
		if c.Status == checkStatusFail {
			status = healthStatusDown
			httpCode = http.StatusServiceUnavailable
			break
		}
	}

	resp := HealthCheck{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    time.Since(h.startTime).String(),
		Checks:    checks,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpCode)
	_ = json.NewEncoder(w).Encode(resp)
}

// formatProviders 把 provider names 拼接成 string（goconst-friendly）
func formatProviders(providers []llm.ProviderName) string {
	if len(providers) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(providers))
	for _, p := range providers {
		parts = append(parts, string(p))
	}
	return strings.Join(parts, ",")
}
