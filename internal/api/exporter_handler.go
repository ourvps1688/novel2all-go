// exporter_handler.go 提供 /api/exporter/* 端点。
//
// 路由：
//
//	GET  /api/exporter/chapter/{n}?format=md|txt|epub|pdf  单章节导出（admin）
//	GET  /api/exporter/formats                          支持的格式
//	POST /api/exporter/render                           渲染任意 markdown（admin）
//
// 设计：导出需要文件系统读章节内容（data/prose/第N章.md），
// 因此 /chapter/{n} 需 admin 鉴权（保护未发布内容）。
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/exporter"
)

// ExporterHandler /api/exporter/* handler
type ExporterHandler struct {
	session UserLookup
}

// NewExporterHandler 创建
func NewExporterHandler(s UserLookup) *ExporterHandler {
	return &ExporterHandler{session: s}
}

// ServeHTTP 路由分发
func (h *ExporterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/exporter")
	path = strings.Trim(path, "/")

	switch path {
	case "":
		http.NotFound(w, r)
	case "formats":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleFormats(w, r)
	case "render":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleRender(w, r)
	default:
		// /chapter/{n} 或 /chapter/{n}/format
		if strings.HasPrefix(path, "chapter/") {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.handleChapter(w, r, strings.TrimPrefix(path, "chapter/"))
		} else {
			http.NotFound(w, r)
		}
	}
}

// handleFormats GET /api/exporter/formats
func (h *ExporterHandler) handleFormats(w http.ResponseWriter, _ *http.Request) {
	formats := exporter.SupportedFormats()
	out := make([]map[string]string, 0, len(formats))
	for _, f := range formats {
		out = append(out, map[string]string{
			"format":    string(f),
			"mime_type": exporterMimeType(f),
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"formats": out,
		"count":   len(out),
	})
}

// RenderRequest POST /api/exporter/render
type RenderRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Format string `json:"format,omitempty"` // 默认 md
}

// handleRender POST /api/exporter/render
func (h *ExporterHandler) handleRender(w http.ResponseWriter, r *http.Request) {
	// admin 鉴权（保护导出资源）
	if !h.requireAdmin(w, r) {
		return
	}
	var req RenderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	format := exporter.ParseFormat(req.Format)
	result, err := exporter.Export(req.Title, req.Body, 0, format)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", result.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=`+strconv.Quote(result.Filename)))
	w.Header().Set("X-Export-Format", string(result.Format))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Body)
}

// handleChapter GET /api/exporter/chapter/{n}?format=md|txt|epub|pdf
func (h *ExporterHandler) handleChapter(w http.ResponseWriter, r *http.Request, rest string) {
	// admin 鉴权
	if !h.requireAdmin(w, r) {
		return
	}

	// rest 可能是 "5" 或 "5/epub"
	parts := strings.Split(rest, "/")
	numStr := parts[0]
	num, err := strconv.Atoi(numStr)
	if err != nil || num < 0 {
		http.Error(w, `{"error":"invalid chapter number"}`, http.StatusBadRequest)
		return
	}

	formatStr := ""
	if len(parts) > 1 {
		formatStr = parts[1]
	} else {
		formatStr = r.URL.Query().Get("format")
	}
	format := exporter.ParseFormat(formatStr)

	// 读 chapter 文件（data/prose/第N章.md）
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	prosePath := chapterProsePath(projectRoot, num)
	data, err := os.ReadFile(prosePath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"chapter %d not found: %v"}`, num, err), http.StatusNotFound)
		return
	}

	result, err := exporter.Export(fmt.Sprintf("第%03d章", num), string(data), num, format)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", result.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=`+strconv.Quote(result.Filename)))
	w.Header().Set("X-Export-Format", string(result.Format))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Body)
}

// requireAdmin 通用 admin 鉴权
func (h *ExporterHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.session == nil {
		http.Error(w, `{"error":"exporter endpoints disabled"}`, http.StatusServiceUnavailable)
		return false
	}
	token := h.session.GetTokenFromRequest(r)
	user, err := h.session.GetUserByToken(r.Context(), token)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	if !auth.IsAdmin(user) {
		http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
		return false
	}
	return true
}

// exporterMimeType mime type helper
func exporterMimeType(f exporter.Format) string {
	switch f {
	case exporter.FormatMD:
		return "text/markdown; charset=utf-8"
	case exporter.FormatTXT:
		return "text/plain; charset=utf-8"
	case exporter.FormatEPUB:
		return "application/epub+zip"
	case exporter.FormatPDF:
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
