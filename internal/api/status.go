package api

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/version"
)

// StatusHandler 提供 GET /api/status
//
// 返回进程级运行时信息（与 Python V1.5.x /api/status schema 部分兼容）。
// 注意：项目级状态（initialized/project_name/genre/total_chapters_target 等）
// 留到 P1-F 下一切片 + chapters package 实现后接入。
type StatusHandler struct {
	startedAt time.Time
}

// NewStatusHandler 创建
func NewStatusHandler() *StatusHandler {
	return &StatusHandler{startedAt: time.Now()}
}

// ServeHTTP 路由分发
func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	resp := map[string]any{
		"initialized":    true,
		"service":        "novel2all-go",
		"version":        version.Version,
		"commit":         version.Commit,
		"go_version":     runtime.Version(),
		"goos":           runtime.GOOS,
		"goarch":         runtime.GOARCH,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
		"goroutines":     runtime.NumGoroutine(),
		"heap_alloc_mb":  float64(mem.HeapAlloc) / 1024 / 1024,
		"heap_sys_mb":    float64(mem.HeapSys) / 1024 / 1024,
		"num_gc":         mem.NumGC,
		"max_procs":      runtime.GOMAXPROCS(0),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
