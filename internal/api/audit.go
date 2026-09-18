// audit.go 提供 /api/audit/* 端点（admin only）。
//
// 路由：
//
//	GET  /api/audit          → 审计日志列表（分页 + 过滤）
//	GET  /api/audit/{id}     → 单条审计日志
//
// 过滤参数：
//
//	?limit=100&offset=0     → 分页（默认 100, max 1000）
//	?event=login            → 按 event_type 过滤
//	?user_id=1              → 按 user_id 过滤
//	?success=true            → 按 success 过滤（true/false）
//	?since=2026-09-01T00:00 → 按 created_at >= since 过滤
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// 字符串常量（goconst 建议）
const boolStrTrue = "true"

// AuditAdminEntryResponse admin /api/audit/* 响应（与 /api/auth/audit 共用基础 schema，但加 user_agent + user_id）
//
// 命名与 auth.go 的 AuditAdminEntryResponse 区分。
type AuditAdminEntryResponse struct {
	ID        int64     `json:"id"`
	EventType string    `json:"event_type"`
	UserID    *int64    `json:"user_id,omitempty"`
	Username  string    `json:"username"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Success   bool      `json:"success"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditAdminListResponse 列表响应（包含分页信息）
type AuditAdminListResponse struct {
	Entries []AuditAdminEntryResponse `json:"entries"`
	Count   int                       `json:"count"`
	Limit   int                       `json:"limit"`
	Offset  int                       `json:"offset"`
}

// AuditHandler /api/audit/* handler
type AuditHandler struct {
	db      *store.DB
	session UserLookup
}

// NewAuditHandler 创建
func NewAuditHandler(db *store.DB, s UserLookup) *AuditHandler {
	return &AuditHandler{db: db, session: s}
}

// ServeHTTP 路由分发 + admin 鉴权
func (h *AuditHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// admin 鉴权
	if !h.requireAdmin(w, r) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/audit")
	path = strings.Trim(path, "/")

	if path == "" {
		// /api/audit
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleList(w, r)
		return
	}

	// /api/audit/{id}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	h.handleGet(w, r, id)
}

// requireAdmin 通用 admin 鉴权（与 state_handler.go 同模式）
func (h *AuditHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.session == nil {
		http.Error(w, `{"error":"audit endpoints disabled"}`, http.StatusServiceUnavailable)
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

// handleList GET /api/audit
//
// Query params:
//   - limit (default 100, max 1000)
//   - offset (default 0)
//   - event (event_type filter, exact match)
//   - user_id (filter by user_id)
//   - success (true|false filter)
//   - since (RFC3339 timestamp, filter created_at >= since)
//
// 注：当前 store.ListAudit 不支持过滤（仅 limit/offset）。
// 切片 11 用 in-memory 过滤（限制 limit=1000 范围内）。
// 后续 P2 阶段改为 SQL WHERE 子句。
func (h *AuditHandler) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := parseLimit(q.Get("limit"), 100)
	offset := parseLimit(q.Get("offset"), 0)

	// 过滤参数
	eventFilter := q.Get("event")
	userIDFilter := parseInt64Ptr(q.Get("user_id"))
	successFilter := parseBoolPtr(q.Get("success"))
	sinceFilter := parseTimePtr(q.Get("since"))

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// 先取 limit*10 条数据（用于 in-memory 过滤）
	// 简化方案：直接取 max 后过滤
	raw, err := h.db.ListAudit(ctx, limit*10, 0)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// 应用过滤
	filtered := raw[:0]
	for _, e := range raw {
		if eventFilter != "" && string(e.EventType) != eventFilter {
			continue
		}
		if userIDFilter != nil && (e.UserID == nil || *e.UserID != *userIDFilter) {
			continue
		}
		if successFilter != nil && e.Success != *successFilter {
			continue
		}
		if sinceFilter != nil && e.CreatedAt.Before(*sinceFilter) {
			continue
		}
		filtered = append(filtered, e)
	}

	// 分页 offset
	start := offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[start:end]

	// 转响应格式
	resp := AuditAdminListResponse{
		Entries: make([]AuditAdminEntryResponse, 0, len(page)),
		Count:   len(filtered),
		Limit:   limit,
		Offset:  offset,
	}
	for _, e := range page {
		resp.Entries = append(resp.Entries, toAuditResponse(e))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleGet GET /api/audit/{id}
func (h *AuditHandler) handleGet(w http.ResponseWriter, r *http.Request, id int64) {
	// 切片 11 简化：ListAudit + 过滤（避免新增 ListAuditByID）
	// 如果 id 找不到返回 404
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	raw, err := h.db.ListAudit(ctx, 1000, 0)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	for _, e := range raw {
		if e.ID == id {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(toAuditResponse(e))
			return
		}
	}

	http.Error(w, `{"error":"audit entry not found"}`, http.StatusNotFound)
}

// toAuditResponse store.AuditEntry → AuditAdminEntryResponse
func toAuditResponse(e *store.AuditEntry) AuditAdminEntryResponse {
	return AuditAdminEntryResponse{
		ID:        e.ID,
		EventType: string(e.EventType),
		UserID:    e.UserID,
		Username:  e.Username,
		IP:        e.IP,
		UserAgent: e.UserAgent,
		Success:   e.Success,
		Detail:    e.Detail,
		CreatedAt: e.CreatedAt,
	}
}

// parseLimit 解析 limit/offset query string
func parseLimit(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	if n > 1000 {
		n = 1000
	}
	return n
}

// parseInt64Ptr 解析 int64 optional
func parseInt64Ptr(s string) *int64 {
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// parseBoolPtr 解析 bool optional
func parseBoolPtr(s string) *bool {
	if s == "" {
		return nil
	}
	switch strings.ToLower(s) {
	case boolStrTrue, "1", "yes":
		v := true
		return &v
	case "false", "0", "no":
		v := false
		return &v
	}
	return nil
}

// parseTimePtr 解析 RFC3339 time optional
func parseTimePtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}
