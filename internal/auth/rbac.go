package auth

import (
	"context"
	"net/http"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// Role 角色
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// IsAdmin 检查用户是否是 admin
func IsAdmin(u *store.User) bool {
	return u != nil && u.Role == string(RoleAdmin)
}

// Middleware 中间件
type Middleware struct {
	manager *SessionManager
}

// NewMiddleware 创建
func NewMiddleware(m *SessionManager) *Middleware {
	return &Middleware{manager: m}
}

// Authenticate 从 cookie 取 token 验证用户，未通过返回 401
// 验证通过则把 user 存到 context
type contextKey string

const userContextKey contextKey = "auth.user"

// RequireAuth 强制认证中间件
func (mw *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := mw.manager.GetTokenFromRequest(r)
		user, err := mw.manager.GetUserByToken(r.Context(), token)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		// 把 user 塞到 context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin 强制 admin 中间件（前置：必须先 RequireAuth）
func (mw *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(userContextKey).(*store.User)
		if !ok || !IsAdmin(user) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetUserFromContext 从 context 取 user（中间件注入的）
func GetUserFromContext(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userContextKey).(*store.User)
	return u, ok
}
