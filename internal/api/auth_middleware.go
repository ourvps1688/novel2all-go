// Package api 提供 novel2all-go HTTP handlers.
//
// auth_middleware.go 实现 Sprint V1.0.1 P0-A: RequireAuth mux-level middleware.
//
// 设计目的:
//   - internal/auth/rbac.go 的 (mw *Middleware) RequireAuth 已经存在,
//     但只接受 *auth.SessionManager (强类型), 无法直接用于 mux-level wrap.
//   - api.RequireAuth 接受更宽的 UserLookup interface, 便于测试 mock.
//   - 用独立 context key (userCtxKey) 避免和 auth.Middleware 内部 key 冲突.
//
// 使用方式 (router.go Commit 3):
//
//	mux.Handle("/api/projects/", RequireAuth(session)(projectsHandler))
//
// 受保护 handler 可通过 UserFromContext(r.Context()) 取当前用户.
package api

import (
	"context"
	"net/http"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// userCtxKey 自定义 context key 类型 (避免和 auth.Middleware.userContextKey 冲突).
//
// 用未导出类型 + 唯一 string 值, 防止其他包意外覆盖.
type userCtxKey struct{}

// userCtxValue 是 context 中存储 *store.User 的哨兵 key.
var userCtxValue = userCtxKey{}

// UserFromContext 从 request context 取 RequireAuth 注入的 *store.User.
//
// 返回 (nil, false) 表示:
//   - endpoint 未经过 RequireAuth wrap (例如 /api/auth/* 公开路径)
//   - 或 wrap chain 中某层覆盖了 context
//
// handler 应把 false 当作 "no auth" 处理 — 通常安全返回 401.
func UserFromContext(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userCtxValue).(*store.User)
	return u, ok
}

// MustUserFromContext 是 UserFromContext 的 panic 版本, 用于
// 已确认 ctx 必带 user 的场景 (例如 RequireAuth 之后的 handler).
//
// 不存在时 panic — 因为这是编程错误 (router 配错), 不应该静默通过.
func MustUserFromContext(ctx context.Context) *store.User {
	u, ok := UserFromContext(ctx)
	if !ok {
		panic("api: context missing user — RequireAuth not wired correctly")
	}
	return u
}

// RequireAuth 返回 mux-level middleware wrapper: 验证 cookie + 注入 user 到 context.
//
// 行为:
//   - session == nil: 写 503 `{"error":"auth disabled"}` + 不调 next (与 RequireAdmin fallback 对齐).
//   - cookie 无效 / user 不存在 / user 被禁用: 写 401 `{"error":"unauthorized"}` + 不调 next.
//   - 成功: 把 *store.User 写入 request context, 调 next.ServeHTTP.
//
// 注意: 不区分 admin / user 角色. 角色检查由 RequireAdmin 单独负责 (Commit 3 P3).
//
// thread-safe: 依赖 UserLookup 实现的并发安全 (SessionManager 内部已加锁).
func RequireAuth(session UserLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if session == nil {
				http.Error(w, `{"error":"auth disabled"}`, http.StatusServiceUnavailable)
				return
			}
			token := session.GetTokenFromRequest(r)
			user, err := session.GetUserByToken(r.Context(), token)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), userCtxValue, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
