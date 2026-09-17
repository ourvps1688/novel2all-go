// Package api 提供 novel2all-go HTTP handlers.
package api

// admin.go 提供 admin-only 路由的鉴权 + 聚合注册.
//
// 设计目的:
//   - router.go 已有很多 registerXxx 子函数, admin-only 路由分散在 4 处
//     (state/metrics_admin/audit/backup), 维护时容易遗漏 admin 鉴权检查
//   - admin.go 把所有 admin-only 路由聚合到 RegisterAdminRoutes,
//     router.go 调用一次即可完成所有 admin 路由注册
//   - RequireAdmin 中间件可单独复用 (例如给非标准 /admin/* 路由加鉴权)
//
// 注意: 这是 P1-F 切片 11+12 的封装整理, 行为不变, 仅重组代码.
import (
	"net/http"

	"github.com/ourvps1688/novel2all-go/internal/auth"
)

// AdminRoutes 聚合所有 admin-only 路由.
//
// RegisterAdminRoutes 一次性注册 state/metrics/audit/backup 等 admin 端点.
func RegisterAdminRoutes(mux *http.ServeMux, deps Deps) {
	// /api/state/* (admin only) - state 持久化管理
	if deps.Session != nil && deps.State != nil {
		mux.Handle("/api/state/", NewStateHandler(deps.State, deps.Session))
	}

	// /api/metrics/reset (admin only) - 重置 metrics counters
	if deps.Session != nil && deps.Metrics != nil {
		mux.Handle("/api/metrics/reset", NewMetricsAdminHandler(deps.Metrics, deps.Session))
	}

	// /api/audit (admin only) - 审计日志查询
	if deps.Session != nil && deps.Store != nil {
		mux.Handle("/api/audit", NewAuditHandler(deps.Store, deps.Session))
	}

	// /api/backup (admin only) - 备份管理
	if deps.Session != nil && deps.Backup != nil {
		mux.Handle("/api/backup", NewBackupHandler(deps.Backup, deps.Session))
	}
}

// RequireAdmin 中间件: 验证 cookie + 必须是 admin 角色.
//
// 通过: 调用 next.ServeHTTP.
// 失败: 写 401/403 + 返回 false, next 不被调用.
//
// 用法:
//
//	mux.Handle("/admin/", RequireAdmin(session, http.HandlerFunc(adminHandler)))
func RequireAdmin(session UserLookup, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if session == nil {
			http.Error(w, `{"error":"admin endpoints disabled"}`, http.StatusServiceUnavailable)
			return
		}
		token := session.GetTokenFromRequest(r)
		user, err := session.GetUserByToken(r.Context(), token)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if !auth.IsAdmin(user) {
			http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
