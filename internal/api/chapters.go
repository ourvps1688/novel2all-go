package api

import (
	"context"
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
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// ChapterHandler 提供 /api/chapters + /api/chapter/* (CRUD + content + save + export)
//
// P1-F 切片 4：基于文件系统的章节管理（项目目录下 prose/第NNN章.md）
// Sprint 15 commit F：filesystem 是 content primary storage，SQLite 加 metadata 索引（list fast + 重启恢复）
// Sprint V1.0.1 P0-B：save + export handler 加 owner check (admin bypass)
//
// 端点：
//
//	GET    /api/chapters                       → 列出项目下所有章节
//	POST   /api/chapter/{N}/                   → 创建章节文件 (P0-B owner check)
//	GET    /api/chapter/{N}/content/           → 读完整内容
//	POST   /api/chapter/{N}/save/              → 保存（手动编辑, P0-B owner check）
//	GET    /api/chapter/{N}/export/            → 导出 (md/txt, P0-B owner check)
//	DELETE /api/chapter/{N}/                   → 删除章节文件
//	POST   /api/chapter/{N}/expand/            → LLM 扩写 (P0-B owner check via actions.projects)
//	POST   /api/chapter/{N}/rewrite/           → LLM 重写 (P0-B owner check via actions.projects)
//	POST   /api/chapter/{N}/review/            → LLM review (P0-B owner check via actions.projects)
//	POST   /api/chapter/{N}/insert/            → LLM 插入 (P0-B owner check via actions.projects)
//	POST   /api/chapter/{N}/rollback/          → 从 .bak 恢复 (P0-B owner check via actions.projects)
type ChapterHandler struct {
	actions *ChapterActions      // LLM 操作组件（可为 nil）
	meta    *store.ChaptersStore // SQLite metadata index（可为 nil，纯 filesystem 模式）

	// Sprint V1.0.1 P0-B: 注入 ProjectsRepo, save/export handler 做 owner check.
	// nil = 禁用 owner check (legacy 模式, 仅用于直接单元测试).
	projects ProjectsRepo
}

// SetMetaStore 注入 SQLite metadata index（Sprint 15 commit F）
//
// 注入后 list 优先查 SQLite metadata；save/delete 同步更新 metadata。
// nil 表示纯 filesystem 模式（向后兼容）。
func (h *ChapterHandler) SetMetaStore(s *store.ChaptersStore) {
	h.meta = s
}

// SetProjects 注入 ProjectsRepo (Sprint V1.0.1 P0-B).
//
// nil = 禁用 owner check (legacy 模式).
// 用法 (router.go):
//
//	h.SetProjects(deps.projectStore)  // SQLite adapter
func (h *ChapterHandler) SetProjects(p ProjectsRepo) {
	h.projects = p
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
	Title     string `json:"title,omitempty"` // Sprint V1.0.1 P2 修复: list 响应包含 title, 否则桌面 fallback "第N章"
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
		// /api/chapter/{N} → POST (create) | DELETE
		switch r.Method {
		case http.MethodPost:
			h.create(w, r, chapter)
		case http.MethodDelete:
			h.delete(w, r, chapter)
		default:
			http.Error(w, "method not allowed (use POST/DELETE /api/chapter/{N}, or /content /save /export)", http.StatusMethodNotAllowed)
		}
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
	case "expand", "rewrite", "review", "insert", actionRollback:
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
//
// Sprint 15 commit F: 如果 SQLite metadata 存在 (SetMetaStore 已注入),
// 优先查 SQLite metadata index (fast + 重启可恢复); 否则 fallback 到 filesystem scan.
func (h *ChapterHandler) list(w http.ResponseWriter, r *http.Request) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}

	// SQLite metadata path (Sprint 15 commit F)
	if h.meta != nil && r.URL.Query().Get("project_id") != "" {
		pid, _ := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64)
		if pid > 0 {
			chapters, err := h.listFromMeta(r.Context(), pid)
			if err == nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"chapters": chapters,
					"count":    len(chapters),
				})
				return
			}
			// fall through to filesystem on error
		}
	}

	chapters, err := h.listFromFS(projectRoot)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"chapters": chapters,
		"count":    len(chapters),
	})
}

// listFromMeta 从 SQLite metadata 取章节列表
func (h *ChapterHandler) listFromMeta(ctx context.Context, projectID int64) ([]ChapterInfo, error) {
	rows, err := h.meta.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ChapterInfo, 0, len(rows))
	for _, r := range rows {
		// 查 filesystem first_line（content_path 存的相对路径, projectRoot 由 query 决定）
		// 这里 first_line 不查 filesystem（Sprint 15 list 不读文件内容, 性能考虑）
		out = append(out, ChapterInfo{
			Chapter:   r.N,
			Filename:  fmt.Sprintf("第%03d章.md", r.N),
			Title:     r.Title, // Sprint V1.0.1 P2: 暴露 title 给桌面列表
			CharCount: r.CharCount,
		})
	}
	return out, nil
}

// listFromFS 从 filesystem 扫 第NNN章.md
func (h *ChapterHandler) listFromFS(projectRoot string) ([]ChapterInfo, error) {
	proseDir := chapterProseDir(projectRoot)
	entries, err := os.ReadDir(proseDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []ChapterInfo{}, nil
		}
		return nil, err
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
	return chapters, nil
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
//
// Sprint 15 commit F: filesystem 写 content + SQLite 同步 upsert metadata
// Sprint V1.0.1 P0-B: 加 owner check (admin bypass), project_id=0 时跳过 (legacy 兼容)
func (h *ChapterHandler) save(w http.ResponseWriter, r *http.Request, chapter int) {
	var req struct {
		Content     string `json:"content"`
		ProjectRoot string `json:"project_root"`
		ProjectID   int64  `json:"project_id,omitempty"`
		Title       string `json:"title,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	// P0-B owner check (最先, 防止 info leak)
	if !h.checkProjectAccess(w, r, req.ProjectID) {
		return
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}
	prosePath := chapterProsePath(req.ProjectRoot, chapter)
	// Sprint V1.0.1 P2: per-file mutex 防止与 expand/rewrite/insert/rollback 交错.
	// save 不读 before (前端送完整内容), 但写仍需排他, 否则与并发 expand 覆盖丢失.
	defer LockChapterFile(prosePath)()
	if err := os.MkdirAll(filepath.Dir(prosePath), 0o755); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(prosePath, []byte(req.Content), 0o644); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	// SQLite metadata upsert (Sprint 15 commit F)
	// 如果 ProjectID > 0 + h.meta 已注入, 同步 metadata
	charCount := len([]rune(req.Content))
	if h.meta != nil && req.ProjectID > 0 {
		// content_path 是相对 projectRoot 的相对路径 (chapterProsePath 返回绝对, 转相对)
		relPath := relChapterPath(req.ProjectRoot, chapter)
		_, _ = h.meta.Upsert(r.Context(), req.ProjectID, chapter, req.Title, relPath, charCount)
	}

	resp := map[string]any{
		"chapter":     chapter,
		"char_count":  charCount,
		"output_path": prosePath,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// create 创建章节（手动新建章节文件 + metadata 同步）
//
// POST /api/chapter/{N}  body: {"project_id": int, "title": string, "content": string}
//
// Sprint V1.0.1 P0-B: 验证 user 对 project 有访问权 (admin bypass).
// project_id=0 时跳过 owner check (legacy 模式).
// 文件已存在 → 409 Conflict (不覆盖).
func (h *ChapterHandler) create(w http.ResponseWriter, r *http.Request, chapter int) {
	var req struct {
		ProjectID int64  `json:"project_id,omitempty"`
		Title     string `json:"title,omitempty"`
		Content   string `json:"content,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if !h.checkProjectAccess(w, r, req.ProjectID) {
		return
	}

	// content 默认空字符串（桌面 app 创建章节时可选填）
	if req.Content == "" {
		req.Content = "# 第" + fmt.Sprintf("%03d", chapter) + "章\n\n"
	}

	// project_root 暂用 "." （Phase 1 mock — 项目无文件系统路径绑定）
	// Phase 2 真实部署时改成从 project_id 查 project table 取 project_root
	projectRoot := "."

	prosePath := chapterProsePath(projectRoot, chapter)

	// Sprint V1.0.1 P2: per-file mutex 防止与并发 save/delete 交错.
	defer LockChapterFile(prosePath)()

	// 409 if file exists
	if _, err := os.Stat(prosePath); err == nil {
		http.Error(w, fmt.Sprintf(`{"error":"chapter %d already exists"}`, chapter), http.StatusConflict)
		return
	}

	if err := os.MkdirAll(filepath.Dir(prosePath), 0o755); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(prosePath, []byte(req.Content), 0o644); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	// SQLite metadata upsert (Sprint 15 commit F)
	charCount := len([]rune(req.Content))
	if h.meta != nil && req.ProjectID > 0 {
		relPath := relChapterPath(projectRoot, chapter)
		_, _ = h.meta.Upsert(r.Context(), req.ProjectID, chapter, req.Title, relPath, charCount)
	}

	resp := map[string]any{
		"chapter":     chapter,
		"title":       req.Title,
		"char_count":  charCount,
		"output_path": prosePath,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// export 导出 (md/txt/epub, P1-F 切片 4 只实现 md + txt)
// Sprint V1.0.1 P0-B: 加 owner check (admin bypass), project_id=0 时跳过 (legacy 兼容)
func (h *ChapterHandler) export(w http.ResponseWriter, r *http.Request, chapter int) {
	// P0-B owner check (最先, 防止 info leak)
	projectIDStr := r.URL.Query().Get("project_id")
	var projectID int64
	if projectIDStr != "" {
		if n, err := strconv.ParseInt(projectIDStr, 10, 64); err == nil {
			projectID = n
		}
	}
	if !h.checkProjectAccess(w, r, projectID) {
		return
	}

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

// checkProjectAccess 验证 user 对 project 有访问权 (Sprint V1.0.1 P0-B owner check helper).
//
// 行为:
//   - user 不在 context → 401 + false (即使 legacy mode 也要求 user)
//   - projectID <= 0 OR h.projects == nil → true (legacy skip, 用于直接单元测试, 仅当 user 已注入)
//   - project 不存在 → 404 + false
//   - admin OR user.ID == project.OwnerID → true
//   - 其他 → 403 + false
//
// save / export handler 入口调用: if !h.checkProjectAccess(w, r, projectID) { return }
func (h *ChapterHandler) checkProjectAccess(w http.ResponseWriter, r *http.Request, projectID int64) bool {
	// 始终先验证 user (即使 legacy mode 也要求 user from context)
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	if projectID <= 0 || h.projects == nil {
		// legacy: 无 project_id 或 projects 仓库未注入, 跳过 owner check
		// (直接单元测试场景, 注入 admin user + project_id=0 即可)
		return true
	}
	project, err := h.projects.Get(projectID)
	if err != nil {
		// Sprint V1.0.1 P2 修复: store 包和 api 包各自定义了 ErrNotFound (不同 var).
		// 必须用 store.ErrNotFound (实际从 store 层返回的错误), errors.Is 才能匹配.
		// 之前的 api.ErrNotFound 在 cross-package 比较时永远 false → 返回 500 + "not found".
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return false
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return false
	}
	if auth.IsAdmin(user) || project.OwnerID == user.ID {
		return true
	}
	http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	return false
}

// delete 删除章节文件
func (h *ChapterHandler) delete(w http.ResponseWriter, r *http.Request, chapter int) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	prosePath := chapterProsePath(projectRoot, chapter)
	// Sprint V1.0.1 P2: per-file mutex 防止与并发 save/expand 交错 (delete vs write).
	defer LockChapterFile(prosePath)()
	if err := os.Remove(prosePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, fmt.Sprintf(`{"error":"chapter %d not found"}`, chapter), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	// Sprint 15 commit F: SQLite metadata 同步删除
	if h.meta != nil {
		pid, _ := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64)
		if pid > 0 {
			_ = h.meta.DeleteByNumber(r.Context(), pid, chapter)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// relChapterPath 返回 chapter 相对 projectRoot 的路径
//
// 例: chapterProsePath("/tmp/p", 1) → "/tmp/p/prose/第001章.md"
//
//	relChapterPath("/tmp/p", 1)     → "prose/第001章.md"
func relChapterPath(projectRoot string, chapter int) string {
	abs := chapterProsePath(projectRoot, chapter)
	if projectRoot == "" || projectRoot == "." {
		return filepath.Join("prose", fmt.Sprintf("第%03d章.md", chapter))
	}
	rel, err := filepath.Rel(projectRoot, abs)
	if err != nil {
		return abs
	}
	return rel
}

// _ silences unused imports for time package when no meta usage
var _ = time.Time{}

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
