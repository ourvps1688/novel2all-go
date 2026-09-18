// Package api 提供 novel2all-go HTTP handlers.

// state_handler.go 提供 /api/state/* 端点（admin only）.
//
// 路由分发：
//
//	GET    /api/state          → 元信息
//	POST   /api/state/save     → 立即保存到 disk
//	POST   /api/state/reload   → 从 disk 重新加载到内存
//	POST   /api/state/reset    → 清空内存 + 删除 state file
//
// Sprint V1.0.1 P3: admin 鉴权已移到 mux-level middleware (RegisterAdminRoutes 包装 RequireAuth+RequireAdmin).
// handler 假设 caller 已过 admin guard, 直接执行业务逻辑.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// StateHandler 提供 /api/state/* 端点（admin only）
type StateHandler struct {
	persistor *StatePersistor
}

// NewStateHandler 创建 StateHandler.
//
// Sprint V1.0.1 P3: 移除 session 参数 (admin 鉴权已移到 mux-level middleware).
func NewStateHandler(p *StatePersistor) *StateHandler {
	return &StateHandler{persistor: p}
}

// ServeHTTP 路由分发 (admin 鉴权在 RegisterAdminRoutes mux-level 完成)
func (h *StateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// nil persistor → 503 (防御性, 与 backup_handler / metrics_admin 一致)
	if h.persistor == nil {
		http.Error(w, `{"error":"state persistor not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/state")
	path = strings.Trim(path, "/")

	switch path {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleInfo(w, r)
	case "save":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleSave(w, r)
	case "reload":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleReload(w, r)
	case "reset":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleReset(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleInfo GET /api/state
func (h *StateHandler) handleInfo(w http.ResponseWriter, _ *http.Request) {
	info := h.persistor.Info()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(info)
}

// handleSave POST /api/state/save
func (h *StateHandler) handleSave(w http.ResponseWriter, _ *http.Request) {
	if err := h.persistor.Save(); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"saved":     true,
		"path":      h.persistor.Path(),
		"timestamp": infoTimestamp(h.persistor.Info().LastSavedAt),
	})
}

// handleReload POST /api/state/reload
func (h *StateHandler) handleReload(w http.ResponseWriter, _ *http.Request) {
	if err := h.persistor.Load(); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	info := h.persistor.Info()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"reloaded":   true,
		"path":       h.persistor.Path(),
		"projects":   info.Projects,
		"cache_hits": info.CacheHits,
	})
}

// handleReset POST /api/state/reset
//
// 谨慎：admin only。
// 行为：清空内存 projects + cache counters + 删除 state file。
func (h *StateHandler) handleReset(w http.ResponseWriter, _ *http.Request) {
	deleted, err := h.persistor.Reset()
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"reset":   true,
		"deleted": deleted,
		"path":    h.persistor.Path(),
	})
}

// infoTimestamp 安全返回 RFC3339 字符串（空时间用空字符串）
func infoTimestamp(t any) string {
	type stringer interface {
		String() string
	}
	if s, ok := t.(stringer); ok {
		return s.String()
	}
	return ""
}
