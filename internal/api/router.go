package api

import (
	"net/http"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/obs"
	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// Deps 注入依赖（避免循环依赖）
type Deps struct {
	Logger   *obs.Logger
	Loader   *skills.Loader
	Router   *llm.Router
}

// Router 返回配置好的 http.ServeMux
//
// P0: /health, /version
// P1: + /api/skills, /api/skills/{name}/execute (SSE), /api/skills/{name}/execute-sync
func Router(deps Deps) *http.ServeMux {
	mux := http.NewServeMux()

	// 健康检查 + 版本
	mux.Handle("/health", NewHealthHandler())
	mux.Handle("/version", NewVersionHandler())

	// Skills API（P1）
	if deps.Loader != nil && deps.Router != nil {
		executor := skills.NewExecutor(deps.Loader, deps.Router)
		skillsHandler := NewSkillsHandler(executor, deps.Loader)
		mux.Handle("/api/skills/", skillsHandler)
	}

	// 根路径提示
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("novel2all-go v0.1.0 — see /health, /version, /api/skills\n"))
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