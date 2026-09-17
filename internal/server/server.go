// Package server 提供 HTTP server 启动逻辑 (Sprint 18 重构).
//
// 设计目的:
//   - cmd/server/main.go 和 cmd/cli/web.go 共享同一套启动逻辑
//   - 之前 server 启动逻辑直接放在 cmd/server/main.go 的 run() 里, 220 行
//   - Sprint 18 把 run() 抽到 internal/server 包, 两个入口调用 Run(cfg)
//
// 注意: 这只是代码组织重构, 行为完全等价, 验证由 9/9 CI 跑过.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/api"
	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/chroma"
	"github.com/ourvps1688/novel2all-go/internal/config"
	"github.com/ourvps1688/novel2all-go/internal/graph"
	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/obs"
	"github.com/ourvps1688/novel2all-go/internal/skills"
	"github.com/ourvps1688/novel2all-go/internal/store"
	"github.com/ourvps1688/novel2all-go/internal/version"
)

// Run 启动 HTTP server 直到收到 SIGINT/SIGTERM 或 fatal error.
//
// 流程:
//  1. 创建 logger
//  2. 初始化 LLM router + skills loader
//  3. 打开 DB + migrate + bootstrap admin
//  4. 创建 metrics + traces
//  5. 注入 LLM cache (L1 内存 + L2 SQLite)
//  6. 创建 projects/chapters store adapter
//  7. 装配 api.Deps + mux
//  8. http.Server.ListenAndServe + 优雅关闭
//
// 阻塞, 不会返回 nil 直到收到 shutdown signal.
func Run(cfg *config.Config) error {
	// 1. logger
	logger := obs.New(cfg.Log.Level, cfg.Log.Format)
	logger.Info("novel2all-go starting",
		"version", version.Version,
		"commit", version.Commit,
		"go_version", version.GoVersion,
	)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// 2. LLM router + skills
	llmRouter := llm.NewRouter(llm.Config{
		DashScopeAPIKey: cfg.LLM.DashScopeAPIKey,
		DeepSeekAPIKey:  cfg.LLM.DeepSeekAPIKey,
		MinimaxAPIKey:   cfg.LLM.MinimaxAPIKey,
		AnthropicAPIKey: cfg.LLM.AnthropicAPIKey,
	})
	availableProviders := llmRouter.AvailableProviders()
	logger.Info("llm_router_initialized",
		"providers", availableProviders,
		"count", len(availableProviders),
	)

	skillLoader, err := skills.NewLoader()
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}
	logger.Info("skills_loaded", "count", skillLoader.Count())

	// 3. DB + admin
	db, err := store.Open(rootCtx, cfg.DB.DSN)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(rootCtx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	logger.Info("db_ready", "dsn", cfg.DB.DSN)

	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_PASS")
	if adminUser != "" && adminPass != "" {
		hash, err := auth.HashPassword(adminPass)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}
		if err := db.EnsureAdminUser(rootCtx, adminUser, hash); err != nil {
			return fmt.Errorf("ensure admin: %w", err)
		}
		logger.Info("admin_user_ready", "username", adminUser)
	} else {
		logger.Warn("admin_user_not_initialized", "hint", "set ADMIN_USER and ADMIN_PASS env vars to bootstrap first admin")
	}

	sessionManager := auth.NewSessionManager(db, auth.DefaultSessionConfig())
	limiter := auth.NewRateLimiter(5, 5*time.Minute, 5*time.Minute)
	logger.Info("auth_ready", "session_ttl", auth.DefaultSessionConfig().TTL.String())

	// 4. metrics + traces
	v := version.Get()
	metrics := obs.NewMetrics(v.Version, v.Commit, v.GoVersion)
	traces := obs.NewTraceRecorder(100)
	logger.Info("ops_ready",
		"metrics_enabled", true,
		"traces_capacity", traces.Capacity(),
	)

	llmRouter.SetMetricsHook(&llmHookAdapter{m: metrics, t: traces})

	// 5. LLM cache (L1 内存 + L2 SQLite)
	cacheStore := store.NewCacheStore(db)
	llmCache := llm.NewCacheWithSQLite(cacheStore, 1024)
	llmRouter.SetCache(llmCache)
	logger.Info("llm_cache_ready", "l1_capacity", 1024, "l2_enabled", true)

	// 6. projects + state
	projectsSQLStore := store.NewProjectsStore(db)
	projectStore := api.NewSQLiteProjectsAdapter(projectsSQLStore)
	statePersistor := api.NewStatePersistor("data/state.json", projectStore)

	chaptersSQLStore := store.NewChaptersStore(db)
	if err := statePersistor.Load(); err != nil {
		logger.Warn("state_load_failed", "error", err.Error(), "action", "starting with empty state")
	} else {
		info := statePersistor.Info()
		logger.Info("state_loaded",
			"path", info.Path,
			"exists", info.Exists,
			"projects", info.Projects,
			"cache_hits", info.CacheHits,
			"last_load_at", info.LastLoadAt,
		)
	}

	// 7. 装配 router
	deps := api.Deps{
		Store:        db,
		Logger:       logger,
		Loader:       skillLoader,
		Router:       llmRouter,
		Session:      sessionManager,
		Limiter:      limiter,
		Metrics:      metrics,
		Traces:       traces,
		State:        statePersistor,
		Backup:       store.NewBackupManager("data/backups", "data/state.json", cfg.DB.DSN, 10),
		Chroma:       chroma.NewClient(128),
		Graph:        graph.NewGraph(),
		ChaptersMeta: chaptersSQLStore,
	}
	deps.SetProjectStore(projectStore)
	mux := api.Router(deps)
	handler := api.LoggingMiddleware(logger, metrics, traces, mux)

	// 8. HTTP server
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http_server_listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutdown_signal_received", "signal", sig.String())
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown_error", "error", err.Error())
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	logger.Info("shutdown_complete")
	return nil
}

// llmHookAdapter 把 obs.Metrics + obs.TraceRecorder 桥接到 llm.MetricsHook 接口.
//
// 注意: 这是 server 内部实现细节, 之前在 cmd/server/llm_hook.go. Sprint 18 重构时
// 合并到 server 包内 (避免跨包暴露接口).
type llmHookAdapter struct {
	m *obs.Metrics
	t *obs.TraceRecorder
}

// IncLLMCall 增加 LLM 调用计数.
func (a *llmHookAdapter) IncLLMCall(provider, task, status string) {
	a.m.IncLLMCall(provider, task, status)
}

// AddLLMTokens 增加 token 计数.
func (a *llmHookAdapter) AddLLMTokens(provider, kind string, n int64) {
	a.m.AddLLMTokens(provider, kind, n)
}

// RecordLLMTrace 记录 LLM 调用 trace.
func (a *llmHookAdapter) RecordLLMTrace(provider, task, status string, duration time.Duration, tokensIn, tokensOut int) {
	a.t.RecordLLM(provider, task, status, duration, tokensIn, tokensOut)
}

// 编译期断言: llmHookAdapter 实现 llm.MetricsHook 接口.
var _ llm.MetricsHook = (*llmHookAdapter)(nil)
