// Command novel2all-go 是 novel2all-go 的 HTTP 服务入口。
//
// 用法：
//
//	novel2all-go [--config configs/.env] [--port 8000]
//
// 环境变量见 configs/.env.example。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/api"
	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/config"
	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/obs"
	"github.com/ourvps1688/novel2all-go/internal/skills"
	"github.com/ourvps1688/novel2all-go/internal/store"
	"github.com/ourvps1688/novel2all-go/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. 命令行参数
	configPath := flag.String("config", "configs/.env", "path to .env file (use empty to skip)")
	flag.Parse()

	// 2. 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 3. 创建 logger
	logger := obs.New(cfg.Log.Level, cfg.Log.Format)
	logger.Info("novel2all-go starting",
		"version", version.Version,
		"commit", version.Commit,
		"go_version", version.GoVersion,
		"config_path", *configPath,
	)

	// 3.5 root context for startup
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// 4. P1 装配：LLM router + Skills loader
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

	// 5. P1-E: Store + Auth
	db, err := store.Open(rootCtx, cfg.DB.DSN)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(rootCtx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	logger.Info("db_ready", "dsn", cfg.DB.DSN)

	// 引导 admin 用户（从环境变量读 ADMIN_USER/ADMIN_PASS；hash 用 bcrypt cost=10）
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

	// 6. 装配 router
	mux := api.Router(api.Deps{
		Store:   db,
		Logger:  logger,
		Loader:  skillLoader,
		Router:  llmRouter,
		Session: sessionManager,
		Limiter: limiter,
	})
	handler := api.LoggingMiddleware(logger, mux)

	// 5. HTTP server
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE 需要长连接
		IdleTimeout:       60 * time.Second,
	}

	// 6. 启动 + 优雅关闭
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

	// 优雅关闭：30s 超时
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown_error", "error", err.Error())
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	logger.Info("shutdown_complete")
	return nil
}
