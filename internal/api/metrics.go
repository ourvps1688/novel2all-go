package api

import (
	"net/http"

	"github.com/ourvps1688/novel2all-go/internal/obs"
)

// MetricsHandler GET /metrics
//
// 输出 Prometheus text format（无鉴权；按惯例由 firewall/反向代理限制访问）。
// Content-Type: text/plain; version=0.0.4; charset=utf-8
type MetricsHandler struct {
	metrics *obs.Metrics
}

// NewMetricsHandler 创建 MetricsHandler。
func NewMetricsHandler(m *obs.Metrics) *MetricsHandler {
	return &MetricsHandler{metrics: m}
}

// ServeHTTP 输出 Prometheus text 格式。
func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if h.metrics == nil {
		http.Error(w, "metrics not initialized", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	// 不显式 WriteHeader(http.StatusOK)：默认 status 就是 200
	_, _ = w.Write([]byte(h.metrics.PrometheusText()))
}
