package api

import (
	"net/http"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/obs"
	"github.com/ourvps1688/novel2all-go/internal/skills"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// Deps 注入依赖（避免循环依赖）
type Deps struct {
	Logger  *obs.Logger
	Loader  *skills.Loader
	Router  *llm.Router
	Session *auth.SessionManager
	Limiter *auth.RateLimiter
	Store   *store.DB // optional - 用于 project share API
}

// Router 返回配置好的 http.ServeMux
//
// P0: /health, /version
// P1: + /api/skills/*
// P1-E: /api/auth/{login,logout,me}
// P1-F: + register, users CRUD, audit
//   - /api/roles
//   - /api/cache/{stats,prompt-stats}
func Router(deps Deps) *http.ServeMux {
	mux := http.NewServeMux()

	// 健康检查 + 版本
	mux.Handle("/health", NewHealthHandler())
	mux.Handle("/version", NewVersionHandler())

	// Auth API（P1-E + P1-F 部分）
	if deps.Session != nil && deps.Limiter != nil {
		authHandler := NewAuthHandler(deps.Session, deps.Limiter)
		mux.Handle("/api/auth/", authHandler)
	}

	// Project share API（P1-F 切片 8）
	// 单独注册到具体 path，避免和 AuthHandler 冲突（ServeMux longest-prefix match）
	if deps.Session != nil && deps.Store != nil {
		shareHandler := NewProjectShareHandler(deps.Session, deps.Store)
		mux.Handle("/api/auth/projects/", shareHandler)
		mux.Handle("/api/auth/users/", shareHandler)
	}

	// Skills API（P1）
	if deps.Loader != nil && deps.Router != nil {
		executor := skills.NewExecutor(deps.Loader, deps.Router)
		skillsHandler := NewSkillsHandler(executor, deps.Loader)
		mux.Handle("/api/skills/", skillsHandler)
	}

	// Roles API（P1-F）
	mux.Handle("/api/roles", NewRolesHandler())

	// Cache API（P1-F mock）
	mux.Handle("/api/cache/", NewCacheHandler())

	// Status API（P1-F）
	mux.Handle("/api/status", NewStatusHandler())

	// Models API（P1-F）
	// 注册 /api/model/ subtree（匹配 /api/model/{current,switch}）
	// + /api/models/ 精确（带 slash，匹配 listModels）
	//   /api/models 不带 slash → ServeMux 自动 301 重定向到 /api/models/
	if deps.Router != nil {
		modelsHandler := NewModelsHandler(deps.Router)
		mux.Handle("/api/model/", modelsHandler)
		mux.Handle("/api/models/", modelsHandler)
	}

	// Projects API（P1-F, 内存版）
	mux.Handle("/api/projects/", NewProjectsHandler())
	// /api/projects 不带 slash → ServeMux 自动 301 重定向到 /api/projects/

	// Write + Tracking API（P1-F 切片 3：流式写作）
	if deps.Loader != nil && deps.Router != nil {
		executor := skills.NewExecutor(deps.Loader, deps.Router)
		writeHandler := NewWriteHandler(executor, deps.Loader)
		mux.Handle("/api/write/", writeHandler)
	}
	mux.Handle("/api/tracking", NewTrackingHandler())

	// Chapters API（P1-F 切片 4+5：文件系统存储 + LLM 操作）
	if deps.Loader != nil && deps.Router != nil {
		executor := skills.NewExecutor(deps.Loader, deps.Router)
		actions := NewChapterActions(executor)
		chaptersHandler := NewChapterHandlerWithActions(actions)
		mux.Handle("/api/chapters", chaptersHandler)
		mux.Handle("/api/chapters/", chaptersHandler)
		mux.Handle("/api/chapter/", chaptersHandler)
	} else {
		chaptersHandler := NewChapterHandler()
		mux.Handle("/api/chapters", chaptersHandler)
		mux.Handle("/api/chapters/", chaptersHandler)
		mux.Handle("/api/chapter/", chaptersHandler)
	}

	// Characters / Relationships / Foreshadows API（P1-F 切片 6：JSON 文件持久化）
	charactersHandler := NewCharactersHandler()
	mux.Handle("/api/characters", charactersHandler)
	mux.Handle("/api/characters/", charactersHandler)
	mux.Handle("/api/relationships", charactersHandler)
	mux.Handle("/api/relationships/", charactersHandler)
	mux.Handle("/api/foreshadows", charactersHandler)
	mux.Handle("/api/foreshadows/", charactersHandler)

	// 根路径提示
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("novel2all-go v0.6.0 — see /health, /version, /api/auth, /api/skills, /api/roles, /api/cache, /api/status, /api/models, /api/projects, /api/write, /api/tracking, /api/chapters, /api/chapter\n"))
	})

	return mux
}

// LoggingMiddleware 简易访问日志中间件
//
// 注意：这是 P0 简化版，P1 会替换为带 request_id + 结构化字段的中间件。
func LoggingMiddleware(logger *obs.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 实现 http.Flusher，转发到底层 ResponseWriter。
// 不实现会导致 SSE handler 断言失败报 "streaming unsupported"。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
