// admin_mux_test.go 提供 admin endpoints (state / metrics_admin / audit / backup) 的
// mux-level 集成测试。
//
// Sprint V1.0.1 P3 Commit 2 删除了 4 个 admin handler 的 inline requireAdmin (鉴权移到
// RegisterAdminRoutes mux-level middleware). 同步删除了 14 个 inline auth/admin tests
// (TestXxx_RequiresAuth / TestXxx_RequiresAdmin / TestXxx_AdminSuccess).
//
// 此文件用 mux-level 方式补回覆盖: 直接构造含 auth + admin endpoints 的 mux,
// 验证 RequireAuth + RequireAdmin 在 RegisterAdminRoutes mux.Handle wrap 中生效.
//
// 覆盖矩阵 (4 endpoints × 4 场景):
//
//   - /api/state             (state handler)
//
//   - /api/state/save        (state handler subpath)
//
//   - /api/audit             (audit handler)
//
//   - /api/metrics/reset     (metrics_admin handler)
//
//   - /api/backup            (backup handler)
//
//   - 无 cookie           → 401 (RequireAuth 拦截)
//
//   - regular user cookie → 403 (RequireAdmin 拦截)
//
//   - admin user cookie   → 200/2xx (handler 真执行业务)
//
//   - nil session         → 404 (RegisterAdminRoutes 早返回, route 未注册)
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/obs"
	"github.com/ourvps1688/novel2all-go/internal/store"
	"github.com/ourvps1688/novel2all-go/internal/testfixtures"
)

// adminEndpoints 是被测的 admin endpoints.
//
// 注: /api/state 用 trailing slash 避免 mux 307 redirect 到 /api/state/.
// /api/backup admin success 测试用 GET 列表 (只读, 无副作用); POST /api/backup 会创建
// 真实 backup 文件, 留作后续 Sprint V1.0.1+ 单独测试.
var adminEndpoints = []string{
	"/api/state/",
	"/api/state/save",
	"/api/audit",
	"/api/metrics/reset",
	"/api/backup",
}

// setupAdminMux 构造含 admin endpoints 的完整 mux (用 api.Router).
//
// 最小依赖: SQLite + SessionManager + RateLimiter + Metrics + State persistor + BackupManager.
// 同时返回 *store.DB 供测试直接 db.CreateUser (创建 admin user, 绕过 /api/auth/register
// 后者只创建 role="user").
func setupAdminMux(t *testing.T) (http.Handler, *store.DB) {
	t.Helper()

	ctx := context.Background()
	db, dsn := openTestDB(t, ctx)
	sm := auth.NewSessionManager(db, auth.DefaultSessionConfig())
	limiter := auth.NewRateLimiter(5, 1*time.Minute, 30*time.Second)
	metrics := obs.NewMetrics("test", "test", "test")

	// state persistor (用于 /api/state/*)
	stateDir := t.TempDir()
	ps := NewProjectStore()
	statePersistor := NewStatePersistor(filepath.Join(stateDir, "state.json"), ps)

	// backup manager (用于 /api/backup, 让 mux route 注册生效).
	// 备份目录用独立 temp dir; state path / db path 指向上面 stateDir 和 dsn.
	backupDir := t.TempDir()
	backupMgr := store.NewBackupManager(backupDir, filepath.Join(stateDir, "state.json"), dsn, 3)

	deps := Deps{
		Store:   db,
		Session: sm,
		Limiter: limiter,
		Metrics: metrics,
		State:   statePersistor,
		Backup:  backupMgr,
	}

	mux := Router(deps)
	if mux == nil {
		t.Fatal("Router 返回 nil")
	}
	return mux, db
}

// openTestDB 打开临时 SQLite db + 跑 migrations + 注册 t.Cleanup 关闭.
//
// 返回的 *store.DB 已 Migrate 过, 测试可直接使用. 同时返回 dsn 路径供 BackupManager 等
// 需要 sqlite 文件路径的调用方使用. 用于 setupAdminMux 和 TestAdminMux_NilSession_RouteNotRegistered
// 共享 db 初始化代码 (避免 dupl linter).
func openTestDB(t *testing.T, ctx context.Context) (*store.DB, string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dsn
}

// createAdminUserAndLogin 直接通过 db.CreateUser 创建 admin user, 然后 LoginAs 拿 cookie.
//
// LoginAs 先 POST /api/auth/register (返回 409 admin 已存在, LoginAs 视为 idempotent OK),
// 然后 POST /api/auth/login 拿 session cookie.
func createAdminUserAndLogin(t *testing.T, mux http.Handler, db *store.DB, username, password string) *http.Cookie {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := db.CreateUser(ctx, username, hash, "admin"); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	return testfixtures.LoginAs(t, mux, username, password)
}

// TestAdminMux_RequiresAuth_NoCookie_401 验证: admin endpoints 无 cookie → 401
// (RequireAuth 在 mux.Handle wrap 中先拦截).
func TestAdminMux_RequiresAuth_NoCookie_401(t *testing.T) {
	mux, _ := setupAdminMux(t)

	for _, ep := range adminEndpoints {
		ep := ep
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, http.NoBody)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s 无 cookie 应 401, 实际 %d body=%s", ep, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAdminMux_RequiresAdmin_RegularUser_403 验证: 带 regular user cookie 但 role!=admin
// → 403 (RequireAdmin 在 RequireAuth 之后拦截).
func TestAdminMux_RequiresAdmin_RegularUser_403(t *testing.T) {
	mux, _ := setupAdminMux(t)
	cookie := testfixtures.LoginAs(t, mux, "bob_reg_v101", "bobpass")

	for _, ep := range adminEndpoints {
		ep := ep
		t.Run(ep, func(t *testing.T) {
			resp := testfixtures.AuthedRequest(t, mux, http.MethodGet, ep, nil, cookie)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s regular user 应 403, 实际 %d", ep, resp.StatusCode)
			}
		})
	}
}

// TestAdminMux_AdminUser_OK 验证: 带 admin user cookie → 200 (auth + admin 双通过,
// handler 真执行业务). /api/backup 用 GET 列表端点 (只读, 验证 admin 鉴权;
// POST /api/backup 会创建真实 backup 文件, 留作后续单独测试).
func TestAdminMux_AdminUser_OK(t *testing.T) {
	mux, db := setupAdminMux(t)
	cookie := createAdminUserAndLogin(t, mux, db, "carol_admin_v101", "carolpass")

	for _, ep := range adminEndpoints {
		ep := ep
		t.Run(ep, func(t *testing.T) {
			method := http.MethodGet
			if ep == "/api/state/save" || ep == "/api/metrics/reset" {
				method = http.MethodPost
			}
			resp := testfixtures.AuthedRequest(t, mux, method, ep, nil, cookie)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s admin user 应 200, 实际 %d", ep, resp.StatusCode)
			}
		})
	}
}

// TestAdminMux_NilSession_RouteNotRegistered 验证: SessionManager=nil 时,
// RegisterAdminRoutes 早返回, admin routes 未注册 → 404 (落到 mux default handler).
//
// 这与 RequireAuth(session=nil) → 503 是两回事:
//   - RegisterAdminRoutes 看到 deps.Session==nil 提前 return (admin.go:79)
//   - 即使 route 被注册, RequireAuth(session=nil) 会写 503
//
// 测试断言 route 未注册时落到 default 404. 这是 Sprint V1.0.1 P3 设计: nil session
// = admin endpoints 完全不可用, 不是返回 503 半可用.
func TestAdminMux_NilSession_RouteNotRegistered(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t, ctx)
	stateDir := t.TempDir()
	ps := NewProjectStore()
	statePersistor := NewStatePersistor(filepath.Join(stateDir, "state.json"), ps)

	// Session=nil + Store=nil + Metrics=nil → RegisterAdminRoutes 早返回
	// (deps.Session==nil). admin routes 全不注册.
	deps := Deps{State: statePersistor, Store: db}
	mux := Router(deps)
	if mux == nil {
		t.Fatal("Router 返回 nil")
	}

	for _, ep := range adminEndpoints {
		ep := ep
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, http.NoBody)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("%s Session=nil route 未注册应 404, 实际 %d body=%s",
					ep, rec.Code, rec.Body.String())
			}
		})
	}
}
