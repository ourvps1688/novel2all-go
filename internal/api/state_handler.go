package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/auth"
)

// StateHandler 提供 /api/state/* 端点（admin only）
//
// 路由分发：
//
//	GET    /api/state          → 元信息
//	POST   /api/state/save     → 立即保存到 disk
//	POST   /api/state/reload   → 从 disk 重新加载到内存
//	POST   /api/state/reset    → 清空内存 + 删除 state file
type StateHandler struct {
	persistor *StatePersistor
	session   UserLookup
}

// NewStateHandler 创建 StateHandler
func NewStateHandler(p *StatePersistor, s UserLookup) *StateHandler {
	return &StateHandler{persistor: p, session: s}
}

// ServeHTTP 路由分发 + admin 鉴权
func (h *StateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// admin 鉴权
	if !h.requireAdmin(w, r) {
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

// requireAdmin 通用 admin 鉴权（失败时写 401/403 响应）
//
// 返回 true = 通过，false = 已写错误响应。
func (h *StateHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.session == nil {
		http.Error(w, `{"error":"state endpoints disabled"}`, http.StatusServiceUnavailable)
		return false
	}
	token := h.session.GetTokenFromRequest(r)
	user, err := h.session.GetUserByToken(r.Context(), token)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	if !auth.IsAdmin(user) {
		http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
		return false
	}
	return true
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

// 防止 unused 警告
var _ = errors.New
