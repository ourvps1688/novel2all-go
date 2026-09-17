package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// ProjectMembershipResp 成员关系响应
type ProjectMembershipResp struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username,omitempty"`
	ProjectPath string `json:"project_path"`
	Role        string `json:"role"`
	GrantedBy   int64  `json:"granted_by"`
	GrantedAt   string `json:"granted_at"`
}

// ProjectShareHandler 提供项目分享 + 用户项目关联 4 个 endpoint
//
// 端点：
//
//	POST   /api/auth/projects/{path:.*}/share/        → 授权用户访问项目
//	DELETE /api/auth/projects/{path:.*}/share/{uid}/   → 撤销用户访问
//	GET    /api/auth/projects/{path:.*}/share/        → 列出项目成员
//	GET    /api/auth/users/{uid}/projects/             → 列出用户能访问的项目
//
// 都需要登录；grant/revoke 需要 admin 权限
type ProjectShareHandler struct {
	session *auth.SessionManager
	store   *store.DB
}

// NewProjectShareHandler 创建
func NewProjectShareHandler(s *auth.SessionManager, db *store.DB) *ProjectShareHandler {
	return &ProjectShareHandler{session: s, store: db}
}

// ServeHTTP 路由分发
//
// 路径格式：/api/auth/projects/{project_root}/share[/{uid}] 或 /api/auth/users/{uid}/projects
func (h *ProjectShareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/auth/")
	path = strings.Trim(path, "/")

	if strings.HasPrefix(path, "projects/") {
		h.handleProjects(w, r, strings.TrimPrefix(path, "projects/"))
		return
	}
	if strings.HasPrefix(path, "users/") {
		h.handleUsers(w, r, strings.TrimPrefix(path, "users/"))
		return
	}
	http.NotFound(w, r)
}

// handleProjects /projects/{path}/share[/{uid}]
func (h *ProjectShareHandler) handleProjects(w http.ResponseWriter, r *http.Request, rest string) {
	// rest 格式: "{path}/share" 或 "{path}/share/{uid}"
	idx := strings.LastIndex(rest, "/share")
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	projectPath := rest[:idx]
	suffix := rest[idx+len("/share"):] // 空 或 "/{uid}"

	// 清理 project_path
	projectPath = cleanProjectPath(projectPath)
	if projectPath == "" {
		http.Error(w, `{"error":"empty project_path"}`, http.StatusBadRequest)
		return
	}

	switch {
	case suffix == "":
		// /api/auth/projects/{path}/share/ → GET list 或 POST grant
		switch r.Method {
		case http.MethodGet:
			h.listMembers(w, r, projectPath)
		case http.MethodPost:
			h.grantAccess(w, r, projectPath)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case strings.HasPrefix(suffix, "/"):
		// /api/auth/projects/{path}/share/{uid}/ → DELETE
		uidStr := strings.TrimPrefix(suffix, "/")
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed (DELETE required)", http.StatusMethodNotAllowed)
			return
		}
		h.revokeAccess(w, r, projectPath, uidStr)
	default:
		http.NotFound(w, r)
	}
}

// handleUsers /users/{uid}/projects/
func (h *ProjectShareHandler) handleUsers(w http.ResponseWriter, r *http.Request, rest string) {
	idx := strings.Index(rest, "/projects")
	if idx < 0 || rest[idx+len("/projects"):] != "" {
		http.NotFound(w, r)
		return
	}
	uidStr := rest[:idx]
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.listUserProjects(w, r, uidStr)
}

// listMembers GET /api/auth/projects/{path}/share/
func (h *ProjectShareHandler) listMembers(w http.ResponseWriter, r *http.Request, projectPath string) {
	if _, ok := h.requireAuth(w, r); !ok {
		return
	}
	members, err := h.store.ListProjectMembers(r.Context(), projectPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	resp := make([]ProjectMembershipResp, 0, len(members))
	for _, m := range members {
		resp = append(resp, membershipWithUsername(r.Context(), h.store, m))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"project_path": projectPath,
		"members":      resp,
		"count":        len(resp),
	})
}

// grantAccess POST /api/auth/projects/{path}/share/  form: user_id, role
func (h *ProjectShareHandler) grantAccess(w http.ResponseWriter, r *http.Request, projectPath string) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid form"}`, http.StatusBadRequest)
		return
	}
	userIDStr := r.FormValue("user_id")
	role := r.FormValue("role")
	if role == "" {
		role = "viewer"
	}
	if userIDStr == "" {
		http.Error(w, `{"error":"user_id required"}`, http.StatusBadRequest)
		return
	}
	var userID int64
	if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
		http.Error(w, `{"error":"invalid user_id"}`, http.StatusBadRequest)
		return
	}
	if !validRole(role) {
		http.Error(w, `{"error":"invalid role (owner/editor/viewer)"}`, http.StatusBadRequest)
		return
	}

	m, err := h.store.GrantProjectAccess(r.Context(), userID, projectPath, role, admin.ID)
	if err != nil {
		if errors.Is(err, store.ErrMembershipExists) {
			http.Error(w, `{"error":"membership already exists"}`, http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(membershipWithUsername(r.Context(), h.store, m))
}

// revokeAccess DELETE /api/auth/projects/{path}/share/{uid}/
func (h *ProjectShareHandler) revokeAccess(w http.ResponseWriter, r *http.Request, projectPath, uidStr string) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var userID int64
	if _, err := fmt.Sscanf(uidStr, "%d", &userID); err != nil {
		http.Error(w, `{"error":"invalid user_id"}`, http.StatusBadRequest)
		return
	}
	if err := h.store.RevokeProjectAccess(r.Context(), userID, projectPath); err != nil {
		if errors.Is(err, store.ErrMembershipNotFound) {
			http.Error(w, `{"error":"membership not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listUserProjects GET /api/auth/users/{uid}/projects/
func (h *ProjectShareHandler) listUserProjects(w http.ResponseWriter, r *http.Request, uidStr string) {
	if _, ok := h.requireAuth(w, r); !ok {
		return
	}
	var userID int64
	if _, err := fmt.Sscanf(uidStr, "%d", &userID); err != nil {
		http.Error(w, `{"error":"invalid user_id"}`, http.StatusBadRequest)
		return
	}
	memberships, err := h.store.ListUserProjects(r.Context(), userID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	resp := make([]ProjectMembershipResp, 0, len(memberships))
	for _, m := range memberships {
		resp = append(resp, membershipWithUsername(r.Context(), h.store, m))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"user_id":  userID,
		"projects": resp,
		"count":    len(resp),
	})
}

// requireAuth 验证已登录（任意用户）
func (h *ProjectShareHandler) requireAuth(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	user, err := h.requireAuthUser(r)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusUnauthorized)
		return nil, false
	}
	return user, true
}

// requireAdmin 验证 admin 权限
func (h *ProjectShareHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	user, ok := h.requireAuth(w, r)
	if !ok {
		return nil, false
	}
	if user.Role != "admin" {
		http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
		return nil, false
	}
	return user, true
}

// requireAuthUser 从 cookie 解析 user
func (h *ProjectShareHandler) requireAuthUser(r *http.Request) (*store.User, error) {
	token := h.session.GetTokenFromRequest(r)
	if token == "" {
		return nil, errors.New("not authenticated")
	}
	if h.session == nil {
		return nil, errors.New("session manager not configured")
	}
	user, err := h.session.GetUserByToken(r.Context(), token)
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	return user, nil
}

// cleanProjectPath 清理 project_path（确保前导 /，去 ./ 和 trailing slash）
func cleanProjectPath(p string) string {
	if p == "" {
		return ""
	}
	p = strings.TrimSpace(p)
	// 去前导 "./"
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	// 确保前导 /
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// 去重复 / (例如 //abc → /abc)
	for strings.HasPrefix(p, "//") && len(p) > 1 {
		p = "/" + p[2:]
	}
	// 去 trailing slash（但保留根 "/"）
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}

// validRole 检查 role 是否合法
func validRole(role string) bool {
	switch role {
	case "owner", "editor", "viewer":
		return true
	}
	return false
}

// membershipWithUsername 添加 username 字段到 membership（enrich）
func membershipWithUsername(ctx context.Context, db *store.DB, m *store.ProjectMembership) ProjectMembershipResp {
	resp := ProjectMembershipResp{
		ID:          m.ID,
		UserID:      m.UserID,
		ProjectPath: m.ProjectPath,
		Role:        m.Role,
		GrantedBy:   m.GrantedBy,
		GrantedAt:   m.GrantedAt,
	}
	// enrich username（失败不影响主响应）
	if user, err := db.GetUserByID(ctx, m.UserID); err == nil {
		resp.Username = user.Username
	}
	return resp
}

// 防止 unused 警告
var _ = io.EOF
