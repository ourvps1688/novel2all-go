// Package api 提供 novel2all-go HTTP handlers.
package api

// admin.go 提供 admin-only 路由的鉴权 + 聚合注册.
//
// 设计目的:
//   - router.go 已有很多 registerXxx 子函数, admin-only 路由分散在 4 处
//     (state/metrics_admin/audit/backup), 维护时容易遗漏 admin 鉴权检查
//   - admin.go 把所有 admin-only 路由聚合到 RegisterAdminRoutes,
//     router.go 调用一次即可完成所有 admin 路由注册
//   - Sprint V1.0.1 P3: 用 RequireAuth + RequireAdmin mux-level middleware 替代各 handler
//     内 inline requireAdmin (DRY + 更易测). 详见 auth_middleware.go.
//
// 注意: 这是 P1-F 切片 11+12 的封装整理, Sprint V1.0.1 P3 把 inline check 移到 middleware 层.
import (
	"net/http"
)

// AdminRoutes 聚合所有 admin-only 路由.
//
// RegisterAdminRoutes 一次性注册 state/metrics/audit/backup 等 admin 端点.
// Sprint V1.0.1 P3: 用 RequireAuth + RequireAdmin chain 替代 inline admin check.
func RegisterAdminRoutes(mux *http.ServeMux, deps Deps) {
	if deps.Session == nil {
		return
	}
	// 链式 wrap: RequireAuth 注入 user + RequireAdmin 检查 admin role
	authGuard := RequireAuth(deps.Session)
	adminGuard := RequireAdmin(deps.Session)

	// /api/state/* (admin only) - state 持久化管理
	if deps.State != nil {
		mux.Handle("/api/state/", authGuard(adminGuard(NewStateHandler(deps.State, deps.Session))))
	}

	// /api/metrics/reset (admin only) - 重置 metrics counters
	if deps.Metrics != nil {
		mux.Handle("/api/metrics/reset", authGuard(adminGuard(NewMetricsAdminHandler(deps.Metrics, deps.Session))))
	}

	// /api/audit (admin only) - 审计日志查询
	if deps.Store != nil {
		mux.Handle("/api/audit", authGuard(adminGuard(NewAuditHandler(deps.Store, deps.Session))))
	}

	// /api/backup (admin only) - 备份管理
	if deps.Backup != nil {
		mux.Handle("/api/backup", authGuard(adminGuard(NewBackupHandler(deps.Backup, deps.Session))))
	}
}
