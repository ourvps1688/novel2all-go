// Package api 提供 HTTP handlers 和路由注册。
//
// P0 阶段只实现健康检查 + 版本端点。
// P1 阶段会加入 auth / projects / chapters / write / skills / cache 等 handlers。
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/version"
)

// HealthResponse /health 端点返回结构
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Uptime    string `json:"uptime"`
}

// HealthHandler 返回服务健康状态
type HealthHandler struct {
	startTime time.Time
}

// NewHealthHandler 创建健康检查 handler
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{startTime: time.Now()}
}

// ServeHTTP 实现 http.Handler 接口
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    time.Since(h.startTime).String(),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// VersionHandler 返回版本信息
type VersionHandler struct{}

// NewVersionHandler 创建版本 handler
func NewVersionHandler() *VersionHandler { return &VersionHandler{} }

// ServeHTTP 实现 http.Handler 接口
func (v *VersionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(version.Get())
}
