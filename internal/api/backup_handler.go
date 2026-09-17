// backup_handler.go 提供 /api/backup/* 端点（admin only）。
//
// 路由：
//
//	POST /api/backup   → 创建新 backup，返回 path + size
//	GET  /api/backup   → 列出所有 backup 文件
//
// 注意：restore endpoint 故意不做（避免误删数据）
// restore 由人工操作：cp data/backups/novel2all-X.tar.gz /tmp/restore && tar -xzf
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// BackupHandler /api/backup/* handler
type BackupHandler struct {
	manager *store.BackupManager
	session UserLookup
}

// NewBackupHandler 创建
func NewBackupHandler(mgr *store.BackupManager, s UserLookup) *BackupHandler {
	return &BackupHandler{manager: mgr, session: s}
}

// ServeHTTP 路由分发 + admin 鉴权
func (h *BackupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// nil manager 不 panic
	if h.manager == nil {
		http.Error(w, `{"error":"backup manager not initialized"}`, http.StatusServiceUnavailable)
		return
	}
	// admin 鉴权
	if !h.requireAdmin(w, r) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/backup")
	path = strings.Trim(path, "/")

	switch path {
	case "":
		switch r.Method {
		case http.MethodPost:
			h.handleCreate(w, r)
		case http.MethodGet:
			h.handleList(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.NotFound(w, r)
	}
}

// requireAdmin 通用 admin 鉴权（与 state_handler / audit_handler 一致模式）
func (h *BackupHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.session == nil {
		http.Error(w, `{"error":"backup endpoints disabled"}`, http.StatusServiceUnavailable)
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

// handleCreate POST /api/backup
func (h *BackupHandler) handleCreate(w http.ResponseWriter, _ *http.Request) {
	result, err := h.manager.Create()
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(result)
}

// handleList GET /api/backup
func (h *BackupHandler) handleList(w http.ResponseWriter, _ *http.Request) {
	backups, err := h.manager.List()
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"backups": backups,
		"count":   len(backups),
		"dir":     h.manager.Dir(),
	})
}
