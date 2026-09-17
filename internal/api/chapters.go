package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ChapterHandler 提供 /api/chapters + /api/chapter/* (CRUD + content + save + export)
//
// P1-F 切片 4：基于文件系统的章节管理（项目目录下 prose/第NNN章.md）
//
// 端点：
//
//	GET    /api/chapters                       → 列出项目下所有章节
//	GET    /api/chapter/{N}/content/           → 读完整内容
//	POST   /api/chapter/{N}/save/              → 保存（手动编辑）
//	GET    /api/chapter/{N}/export/            → 导出 (md/txt)
//	DELETE /api/chapter/{N}/                   → 删除章节文件
//	POST   /api/chapter/{N}/expand/            → LLM 扩写
//	POST   /api/chapter/{N}/rewrite/           → LLM 重写
//	POST   /api/chapter/{N}/review/            → LLM review
//	POST   /api/chapter/{N}/insert/            → LLM 插入
//	POST   /api/chapter/{N}/rollback/          → 从 .bak 恢复
type ChapterHandler struct {
	actions *ChapterActions // LLM 操作组件（可为 nil）
}

const (
	formatMD  = "md"
	formatTXT = "txt"
)

// mimeTypeMD 是导出时的 Content-Type（goconst 避免字面量重复）
const mimeTypeMD = "text/markdown; charset=utf-8"

// NewChapterHandler 创建（无 LLM 操作）
func NewChapterHandler() *ChapterHandler { return &ChapterHandler{} }

// NewChapterHandlerWithActions 创建（含 LLM 操作）
func NewChapterHandlerWithActions(actions *ChapterActions) *ChapterHandler {
	return &ChapterHandler{actions: actions}
}

// ChapterInfo 章节元信息（列表项）
type ChapterInfo struct {
	Chapter   int    `json:"chapter"`
	Filename  string `json:"filename"`
	CharCount int    `json:"char_count"`
	FirstLine string `json:"first_line,omitempty"`
}

// ChapterContent 章节完整内容
type ChapterContent struct {
	Chapter   int    `json:"chapter"`
	Filename  string `json:"filename"`
	Content   string `json:"content"`
	CharCount int    `json:"char_count"`
	FirstLine string `json:"first_line"`
}

// ServeHTTP 路由分发（CRUD + LLM actions）
//
// 注册路径：
//
//	/api/chapters          → list（精确）
//	/api/chapters/         → list（带 slash）
//	/api/chapter/          → subtree，匹配其他
//
//nolint:gocyclo // 路由分发天然多分支（CRUD 5 个 action + LLM 5 个 action）
func (h *ChapterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// /api/chapters (list) - 单独 path
	if r.URL.Path == "/api/chapters" || r.URL.Path == "/api/chapters/" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.list(w, r)
		return
	}

	// /api/chapter/{N}[/{action}]
	// 去掉前缀 /api/chapter
	path := strings.TrimPrefix(r.URL.Path, "/api/chapter")
	path = strings.Trim(path, "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(path, "/")
	chapter, err := strconv.Atoi(parts[0])
	if err != nil || chapter <= 0 {
		http.Error(w, `{"error":"invalid chapter number"}`, http.StatusBadRequest)
		return
	}

	if len(parts) == 1 {
		// /api/chapter/{N} → DELETE
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed (use /content, /save, /export)", http.StatusMethodNotAllowed)
			return
		}
		h.delete(w, r, chapter)
		return
	}

	// /api/chapter/{N}/{action}
	action := parts[1]
	switch action {
	case "content":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.content(w, r, chapter)
	case "save":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.save(w, r, chapter)
	case "export":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.export(w, r, chapter)
	case "expand", "rewrite", "review", "insert", "rollback":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed (POST required)", http.StatusMethodNotAllowed)
			return
		}
		if h.actions == nil {
			http.Error(w, `{"error":"LLM actions not configured"}`, http.StatusServiceUnavailable)
			return
		}
		h.actions.DispatchAction(w, r, chapter, action)
	default:
		http.NotFound(w, r)
	}
}

// list 列出项目下所有章节
func (h *ChapterHandler) list(w http.ResponseWriter, r *http.Request) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	proseDir := chapterProseDir(projectRoot)
	entries, err := os.ReadDir(proseDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// 项目未初始化 → 空列表
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"chapters": []ChapterInfo{},
				"count":    0,
			})
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	re := regexp.MustCompile(`^第(\d+)章\.md$`)
	var chapters []ChapterInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := re.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		ch, _ := strconv.Atoi(m[1])
		fullPath := filepath.Join(proseDir, entry.Name())
		data, _ := os.ReadFile(fullPath)
		text := string(data)
		firstLine := strings.SplitN(text, "\n", 2)[0]
		firstLine = strings.TrimSpace(firstLine)
		if len(firstLine) > 120 {
			firstLine = firstLine[:120]
		}
		chapters = append(chapters, ChapterInfo{
			Chapter:   ch,
			Filename:  entry.Name(),
			CharCount: len([]rune(text)),
			FirstLine: firstLine,
		})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"chapters": chapters,
		"count":    len(chapters),
	})
}

// content 读取完整章节
func (h *ChapterHandler) content(w http.ResponseWriter, r *http.Request, chapter int) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	prosePath := chapterProsePath(projectRoot, chapter)
	data, err := os.ReadFile(prosePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, fmt.Sprintf(`{"error":"chapter %d not found"}`, chapter), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	text := string(data)
	firstLine := strings.SplitN(text, "\n", 2)[0]
	firstLine = strings.TrimSpace(firstLine)
	if len(firstLine) > 120 {
		firstLine = firstLine[:120]
	}
	resp := ChapterContent{
		Chapter:   chapter,
		Filename:  fmt.Sprintf("第%03d章.md", chapter),
		Content:   text,
		CharCount: len([]rune(text)),
		FirstLine: firstLine,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// save 保存（手动编辑）
func (h *ChapterHandler) save(w http.ResponseWriter, r *http.Request, chapter int) {
	var req struct {
		Content     string `json:"content"`
		ProjectRoot string `json:"project_root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}
	prosePath := chapterProsePath(req.ProjectRoot, chapter)
	if err := os.MkdirAll(filepath.Dir(prosePath), 0o755); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(prosePath, []byte(req.Content), 0o644); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"chapter":     chapter,
		"char_count":  len([]rune(req.Content)),
		"output_path": prosePath,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// export 导出 (md/txt/epub, P1-F 切片 4 只实现 md + txt)
func (h *ChapterHandler) export(w http.ResponseWriter, r *http.Request, chapter int) {
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = formatMD
	}
	if format != formatMD && format != "txt" {
		http.Error(w, fmt.Sprintf(`{"error":"unsupported format: %s (only md/txt in P1-F)"}`, format), http.StatusBadRequest)
		return
	}

	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	prosePath := chapterProsePath(projectRoot, chapter)
	data, err := os.ReadFile(prosePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, fmt.Sprintf(`{"error":"chapter %d not found"}`, chapter), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	content := string(data)
	var (
		mimeType string
		body     []byte
		ext      string
	)

	switch format {
	case formatMD:
		mimeType = mimeTypeMD
		body = []byte(content)
		ext = formatMD
	case formatTXT:
		mimeType = "text/plain; charset=utf-8"
		body = []byte(stripMarkdown(content))
		ext = formatTXT
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="chapter_%03d.%s"`, chapter, ext))
	_, _ = w.Write(body)
}

// delete 删除章节文件
func (h *ChapterHandler) delete(w http.ResponseWriter, r *http.Request, chapter int) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	prosePath := chapterProsePath(projectRoot, chapter)
	if err := os.Remove(prosePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, fmt.Sprintf(`{"error":"chapter %d not found"}`, chapter), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// chapterProseDir 返回项目下 prose/ 目录
func chapterProseDir(projectRoot string) string {
	return filepath.Join(projectRoot, "prose")
}

// chapterProsePath 返回章节文件路径
func chapterProsePath(projectRoot string, chapter int) string {
	return filepath.Join(chapterProseDir(projectRoot), fmt.Sprintf("第%03d章.md", chapter))
}

// stripMarkdown 简单剥离 markdown 标记（仅用于 .txt 导出）
// 移除: 标题标记(#) / 加粗(**) / 斜体(*) / 链接([text](url)) / 代码(`) / HTML 标签
func stripMarkdown(s string) string {
	// 移除围栏代码块 ```
	s = regexp.MustCompile("(?s)```[^`]*```").ReplaceAllString(s, "")
	// 移除行内代码 `xx`
	s = regexp.MustCompile("`([^`]*)`").ReplaceAllString(s, "$1")
	// 移除图片 ![alt](url)
	s = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(s, "$1")
	// 链接 [text](url) → text
	s = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`).ReplaceAllString(s, "$1")
	// 加粗 **xx** / __xx__
	s = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`__([^_]+)__`).ReplaceAllString(s, "$1")
	// 斜体 *xx* / _xx_
	s = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`(?m)\b_([^_]+)_\b`).ReplaceAllString(s, "$1")
	// 标题 # / ## / ### (保留文字)
	s = regexp.MustCompile(`(?m)^#+\s+`).ReplaceAllString(s, "")
	// HTML 标签
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}
