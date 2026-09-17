package api

import (
	"net/http"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/chroma"
	"github.com/ourvps1688/novel2all-go/internal/graph"
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
	// Store 用于 project share API
	Store *store.DB

	// P1-F 切片 9: 运维可观测性
	// Metrics 用于 /metrics + /debug/info
	Metrics *obs.Metrics
	// Traces 用于 /debug/traces
	Traces *obs.TraceRecorder

	// P1-F 切片 10: state 持久化
	// State 用于 /api/state/* (save/load/reset)
	State *StatePersistor

	// P1-F 切片 12: backup
	// Backup 用于 /api/backup (admin only)
	Backup *store.BackupManager

	// P1-C: chroma (向量存储) + graph (图算法)
	// Chroma 用于 /api/chroma/* (RAG 基础设施)
	Chroma *chroma.Client
	// Graph 用于 /api/graph/* (人物关系图谱)
	Graph *graph.Graph

	// projectStore 共享 ProjectStore（切片 10 让 State 持久化 projects）
	// 未导出避免 main.go 误用（应该只通过 State 间接访问）
	// 用 SetProjectStore 方法设置（main.go 在外部构造 Deps）
	projectStore *ProjectStore
}

// SetProjectStore 设置共享 ProjectStore（P1-F 切片 10 用）
//
// main.go 在外部构造 ProjectStore + StatePersistor, 然后用此方法注入。
// router.go 用 projectStore 给 ProjectsHandler 共享同一个 store。
//
// 参数名 ps 避免 shadow 'store' package 名（gocritic importShadow）。
func (d *Deps) SetProjectStore(ps *ProjectStore) {
	d.projectStore = ps
}

// Router 返回配置好的 http.ServeMux
//
// P0: /health, /version
// P1: + /api/skills/*
// P1-E: /api/auth/{login,logout,me}
// P1-F: + register, users CRUD, audit
//   - /api/roles
//   - /api/cache/{stats,prompt-stats}
//   - /api/model, /api/models, /api/projects, /api/write, /api/tracking
//   - /api/chapters, /api/chapter, /api/characters, /api/relationships, /api/foreshadows
//   - /metrics, /debug/* (切片 9)
//   - /health/live, /health/ready (切片 11)
//   - /api/audit/* (切片 11)
func Router(deps Deps) *http.ServeMux {
	mux := http.NewServeMux()

	registerSystemRoutes(mux, deps)
	registerAuthRoutes(mux, deps)
	registerContentRoutes(mux, deps)
	registerProjectRoutes(mux, deps)
	registerOpsRoutes(mux, deps)
	registerRootHandler(mux)

	return mux
}

// registerSystemRoutes 注册 /health + /version + /health/live + /health/ready
func registerSystemRoutes(mux *http.ServeMux, deps Deps) {
	// 基础 /health（保留向后兼容）
	mux.Handle("/health", NewHealthHandler())
	mux.Handle("/version", NewVersionHandler())
	// /health/live + /health/ready（切片 11 深度健康检查）
	mux.Handle("/health/", NewHealthReadyHandler(deps.Store, deps.Router))
}

// registerAuthRoutes 注册 auth + project share 路由
func registerAuthRoutes(mux *http.ServeMux, deps Deps) {
	if deps.Session != nil && deps.Limiter != nil {
		authHandler := NewAuthHandler(deps.Session, deps.Limiter)
		mux.Handle("/api/auth/", authHandler)
	}

	// Project share API（单独注册避免和 AuthHandler 冲突）
	if deps.Session != nil && deps.Store != nil {
		shareHandler := NewProjectShareHandler(deps.Session, deps.Store)
		mux.Handle("/api/auth/projects/", shareHandler)
		mux.Handle("/api/auth/users/", shareHandler)
	}
}

// registerContentRoutes 注册 skills + roles + cache + status + models + chapters + characters
func registerContentRoutes(mux *http.ServeMux, deps Deps) {
	// Skills API
	if deps.Loader != nil && deps.Router != nil {
		executor := skills.NewExecutor(deps.Loader, deps.Router)
		skillsHandler := NewSkillsHandler(executor, deps.Loader)
		mux.Handle("/api/skills/", skillsHandler)
	}

	// Roles API
	mux.Handle("/api/roles", NewRolesHandler())

	// Cache API
	mux.Handle("/api/cache/", NewCacheHandler())

	// Status API
	mux.Handle("/api/status", NewStatusHandler())

	// Models API（/api/model/{current,switch} + /api/models/）
	if deps.Router != nil {
		modelsHandler := NewModelsHandler(deps.Router)
		mux.Handle("/api/model/", modelsHandler)
		mux.Handle("/api/models/", modelsHandler)
	}

	// Chapters API（filesystem + LLM actions）
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

	// Characters + Relationships + Foreshadows API（JSON 持久化）
	charactersHandler := NewCharactersHandler()
	mux.Handle("/api/characters", charactersHandler)
	mux.Handle("/api/characters/", charactersHandler)
	mux.Handle("/api/relationships", charactersHandler)
	mux.Handle("/api/relationships/", charactersHandler)
	mux.Handle("/api/foreshadows", charactersHandler)
	mux.Handle("/api/foreshadows/", charactersHandler)
}

// registerProjectRoutes 注册 projects + write + tracking
func registerProjectRoutes(mux *http.ServeMux, deps Deps) {
	// Projects API（内存版）
	// 共享 ProjectStore: 切片 10 让 StatePersistor 可以持久化 projects
	mux.Handle("/api/projects/", NewProjectsHandlerWithStore(deps.projectStore))

	// Write + Tracking API（流式写作）
	if deps.Loader != nil && deps.Router != nil {
		executor := skills.NewExecutor(deps.Loader, deps.Router)
		writeHandler := NewWriteHandler(executor, deps.Loader)
		mux.Handle("/api/write/", writeHandler)
	}
	mux.Handle("/api/tracking", NewTrackingHandler())
}

// registerOpsRoutes 注册 metrics + debug（切片 9）+ state + metrics admin（切片 10）+ audit（切片 11）
func registerOpsRoutes(mux *http.ServeMux, deps Deps) {
	// /metrics 无鉴权（Prometheus 惯例；用 firewall/reverse-proxy 限制）
	if deps.Metrics != nil {
		mux.Handle("/metrics", NewMetricsHandler(deps.Metrics))
	}
	// /debug/* 需要 admin 鉴权
	if deps.Session != nil && (deps.Metrics != nil || deps.Traces != nil) {
		debugHandler := NewDebugHandler(deps.Session, deps.Metrics, deps.Traces)
		mux.Handle("/debug/", debugHandler)
	}
	// /api/state/* state 持久化（admin only）
	if deps.Session != nil && deps.State != nil {
		stateHandler := NewStateHandler(deps.State, deps.Session)
		mux.Handle("/api/state/", stateHandler)
	}
	// /api/metrics/reset metrics admin（admin only）
	if deps.Session != nil && deps.Metrics != nil {
		metricsAdminHandler := NewMetricsAdminHandler(deps.Metrics, deps.Session)
		mux.Handle("/api/metrics/reset", metricsAdminHandler)
	}
	// /api/audit/* 审计日志查询（admin only，require DB）
	if deps.Session != nil && deps.Store != nil {
		auditHandler := NewAuditHandler(deps.Store, deps.Session)
		mux.Handle("/api/audit", auditHandler)
	}
	// /api/backup/* 备份管理（admin only，require BackupManager）
	if deps.Session != nil && deps.Backup != nil {
		backupHandler := NewBackupHandler(deps.Backup, deps.Session)
		mux.Handle("/api/backup", backupHandler)
	}
	// P1-C: /api/chroma/* 向量存储 (RAG)
	if deps.Chroma != nil {
		mux.Handle("/api/chroma/", NewChromaHandler(deps.Chroma))
	}
	// P1-C: /api/graph/* 图算法 (BFS/Dijkstra)
	if deps.Graph != nil {
		mux.Handle("/api/graph/", NewGraphHandler(deps.Graph))
	}
	// P1-D: /api/exporter/* 多格式导出 (md/txt/epub/pdf)
	if deps.Session != nil {
		mux.Handle("/api/exporter/", NewExporterHandler(deps.Session))
	}
}

// registerRootHandler 注册根路径提示
func registerRootHandler(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("novel2all-go v0.6.0 — see /health, /version, /api/auth, /api/skills, /api/roles, /api/cache, /api/status, /api/models, /api/projects, /api/write, /api/tracking, /api/chapters, /api/chapter\n"))
	})
}

// LoggingMiddleware 简易访问日志中间件 + metrics 注入
//
// 注意：这是 P0 简化版，P1 会替换为带 request_id + 结构化字段的中间件。
// P1-F 切片 9 增加 metrics + traces 写入。
func LoggingMiddleware(logger *obs.Logger, metrics *obs.Metrics, traces *obs.TraceRecorder, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		duration := time.Since(start)
		logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.status,
			"duration_ms", duration.Milliseconds(),
			"remote", r.RemoteAddr,
		)
		// 写 metrics（可选）
		if metrics != nil {
			metrics.IncHTTPRequests(r.Method, r.URL.Path, ww.status)
			metrics.ObserveHTTPDuration(r.Method, r.URL.Path, duration)
		}
		// 写 trace（可选）
		if traces != nil {
			traces.RecordHTTP(r.Method, r.URL.Path, ww.status, duration)
		}
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
