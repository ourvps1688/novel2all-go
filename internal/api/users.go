// Package api 提供 novel2all-go HTTP handlers.
package api

// users.go 提供 /api/auth/users CRUD (admin only).
//
// 设计目的:
//   - auth.go 之前把 login/logout/me/register/users/audit 全混在一个 AuthHandler,
//     gocyclo 高且单一职责不清晰
//   - Sprint 17 把 users CRUD 抽到独立 UsersHandler, auth.go 只负责 auth lifecycle
//   - 路径仍是 /api/auth/users, 保持向后兼容
//
// 路由:
//
//	GET    /api/auth/users       → 列表 (admin)
//	POST   /api/auth/users       → 创建 (admin)
//	DELETE /api/auth/users/{id}  → 删除 (admin, 防 self-delete)
//
// 鉴权: 全部 admin guard, 通过 session.GetUserByToken + auth.IsAdmin.
import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// UsersHandler 处理 /api/auth/users/* (admin only).
type UsersHandler struct {
	manager *auth.SessionManager
}

// NewUsersHandler 创建.
func NewUsersHandler(m *auth.SessionManager) *UsersHandler {
	return &UsersHandler{manager: m}
}

// UserResponse 单个用户响应 (与 auth.go 旧定义一致).
type UserResponse struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
}

// UsersListResponse 列表响应.
type UsersListResponse struct {
	Users []UserResponse `json:"users"`
	Count int            `json:"count"`
}

// CreateUserRequest POST /api/auth/users 请求体 (admin guard).
//
// 字段 Role 可省略, 默认 "user".
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

// CreateUserResponse POST /api/auth/users 响应.
type CreateUserResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// ServeHTTP 路由分发.
//
//	GET    ""      → listUsers
//	POST   ""      → createUser
//	DELETE "{id}"  → deleteUser
//
//nolint:gocyclo // users 路由多分支 (method 检查 + 子路径解析)
func (h *UsersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/auth/users")
	path = strings.Trim(path, "/")

	if path == "" {
		switch r.Method {
		case http.MethodGet:
			h.listUsers(w, r)
		case http.MethodPost:
			h.createUser(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/auth/users/{id} → DELETE
	idStr := strings.TrimSuffix(path, "/")
	if r.Method == http.MethodDelete {
		h.deleteUser(w, r, idStr)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// listUsers GET /api/auth/users (admin guard).
func (h *UsersHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}

	users, err := h.manager.ListUsers(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	resp := UsersListResponse{Users: make([]UserResponse, 0, len(users)), Count: len(users)}
	for _, u := range users {
		resp.Users = append(resp.Users, UserResponse{
			ID:        u.ID,
			Username:  u.Username,
			Role:      u.Role,
			Disabled:  u.Disabled,
			CreatedAt: u.CreatedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// createUser POST /api/auth/users (admin guard).
//
// Admin 可指定 role, 默认 "user".
func (h *UsersHandler) createUser(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %v"}`, err), http.StatusBadRequest)
		return
	}

	role := req.Role
	if role == "" {
		role = roleUser
	}
	if role != roleAdmin && role != roleUser {
		http.Error(w, fmt.Sprintf(`{"error":"invalid role: %q"}`, role), http.StatusBadRequest)
		return
	}

	user, err := h.manager.Register(r.Context(), auth.RegisterInput{
		Username: req.Username,
		Password: req.Password,
		Role:     role,
	})
	if err != nil {
		if errors.Is(err, store.ErrUserExists) {
			http.Error(w, `{"error":"username already taken"}`, http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(CreateUserResponse{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
	})
}

// deleteUser DELETE /api/auth/users/{id} (admin guard, self-delete 防护).
func (h *UsersHandler) deleteUser(w http.ResponseWriter, r *http.Request, idStr string) {
	requestor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid user id"}`, http.StatusBadRequest)
		return
	}

	u, err := h.manager.GetUserByID(r.Context(), id)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusNotFound)
		return
	}

	if err := h.manager.DeleteUser(r.Context(), requestor, u); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// requireAdmin 验证 cookie + 必须是 admin.
//
// 通过返回 *store.User; 不通过写 401/403 + 返回 error.
// 这是 UsersHandler 的私有方法 (其他 handler 有各自的实现, 避免循环依赖).
func (h *UsersHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (*store.User, error) {
	token := h.manager.GetTokenFromRequest(r)
	user, err := h.manager.GetUserByToken(r.Context(), token)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return nil, err
	}
	if !auth.IsAdmin(user) {
		http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
		return nil, fmt.Errorf("admin required")
	}
	return user, nil
}
