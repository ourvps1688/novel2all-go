// Package api 提供 novel2all-go HTTP handlers.
package api

// static.go 提供 SPA (Single Page Application) 静态文件服务.
//
// 设计目的:
//   - 当前后端只提供 /api/* JSON 端点, 没有前端
//   - 未来 P2-A 接入 React SPA 时, 需要 serve 前端 dist 目录
//   - StaticHandler 处理 /assets/... 实际文件 + fallback 到 index.html
//     (这样 React Router 的 client-side 路由 /projects/123 也能正确响应)
//
// 行为:
//   - GET /assets/foo.js → 读 web/dist/assets/foo.js, Content-Type 按扩展名
//   - GET /              → 读 web/dist/index.html
//   - GET /projects/123   → 读 web/dist/index.html (SPA fallback)
//   - GET /api/...        → 不进 StaticHandler, 由其他 mux 路由处理
//   - 文件不存在           → 404 (兜底, 即使有 SPA fallback 也优先返回真实 404)
//
// 注意: 这是 P1 阶段的脚手架, 当前可能没有 web/dist 目录, 那时所有 GET 都会 404.
// P2-A 接入前端 dist 时直接复用, 无需改代码.
import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// StaticHandler serve 静态文件 + SPA fallback.
//
// 参数 dir 是前端 dist 目录的绝对路径 (e.g. "web/dist").
// 若 dir 为空字符串, handler 始终返回 404 (用于前端尚未构建的场景).
type StaticHandler struct {
	dir     string
	indexFn string // 默认 "index.html"
}

// NewStaticHandler 创建 handler.
//
// dir 不存在不报错, 只是请求会 404.
func NewStaticHandler(dir string) *StaticHandler {
	return &StaticHandler{dir: dir, indexFn: "index.html"}
}

// ServeHTTP 处理 GET 请求.
//
// 1. 路径安全检查 (防 ../ 越权)
// 2. 实际文件 → 直接 serve
// 3. 找不到 → fallback 到 index.html (SPA route)
// 4. dir 为空 → 404
func (h *StaticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.dir == "" {
		http.NotFound(w, r)
		return
	}

	// 清理路径
	cleanPath := filepath.Clean(r.URL.Path)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(h.dir, cleanPath)

	// 1. 实际文件 → serve
	if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
		h.serveFile(w, r, fullPath)
		return
	}

	// 2. SPA fallback → index.html
	indexPath := filepath.Join(h.dir, h.indexFn)
	if _, err := os.Stat(indexPath); err == nil {
		h.serveFile(w, r, indexPath)
		return
	}

	// 3. 兜底 404
	http.NotFound(w, r)
}

// serveFile 写文件 + Content-Type.
func (h *StaticHandler) serveFile(w http.ResponseWriter, r *http.Request, path string) {
	ext := filepath.Ext(path)
	ctype := mime.TypeByExtension(ext)
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-cache")

	data, err := os.ReadFile(path) // #nosec G304 -- path 已 Clean+Join 防越权
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data)
}
