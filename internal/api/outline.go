package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// OutlineItem 章节大纲 (Module E - 2026-09-20).
//
// 数据模型 (单一层级, 每项对应一章):
//   - chapter: 章节号 (1-based, 必须唯一)
//   - title:   章节标题
//   - summary: 章节梗概 (一两段)
//   - key_events: 关键事件列表 (按顺序发生)
//   - status: planned / in_progress / done
//   - notes: 作者备注 (创作时提醒自己)
//   - characters: 涉及人物名 (与 Module D characters 关联, 但弱类型用名字引用)
//   - foreshadows: 伏笔名 (与 Module D foreshadows 关联, 同样名字引用)
//
// 存储: {project_root}/outline.json (Phase 1 mock, 同 characters.go 模式).
// 进程级 RWMutex 保护并发写 (与 ChapterLock 文件锁独立, 防止多桌面实例覆盖).
type OutlineItem struct {
	ID          int64    `json:"id"`
	ProjectID   int64    `json:"project_id"`
	Chapter     int      `json:"chapter"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary,omitempty"`
	KeyEvents   []string `json:"key_events,omitempty"`
	Status      string   `json:"status"`
	Notes       string   `json:"notes,omitempty"`
	Characters  []string `json:"characters,omitempty"`
	Foreshadows []string `json:"foreshadows,omitempty"`
}

// outlineStorage 项目级 outline 内存存储 (per-process, 加锁保护).
//
// key = project_root 字符串 (e.g. "."), value = 项目所有 outline items.
// 多桌面实例 / 多请求并发时, 同一项目 root 串行化写.
var (
	outlineStorageMu sync.RWMutex
	outlineStorage   = make(map[string][]OutlineItem)
)

const (
	outlineStatusPlanned     = "planned"
	outlineStatusInProgress  = "in_progress"
	outlineStatusDone        = "done"
	defaultOutlineFileName    = "outline.json"
)

// OutlineHandler /api/outline + /api/outline/{id} CRUD.
//
// 端点:
//   GET    /api/outline?project_root=.     → list
//   POST   /api/outline                     → create (body 含 project_id + 字段)
//   GET    /api/outline/{id}                → get one
//   PUT    /api/outline/{id}                → update (完整 body 替换, 强制 ID 一致)
//   DELETE /api/outline/{id}                → delete, 返 204
type OutlineHandler struct{}

func NewOutlineHandler() *OutlineHandler { return &OutlineHandler{} }

func (h *OutlineHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/outline")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")

	switch len(parts) {
	case 0:
		// 集合端点 /api/outline (可带 ?project_root= 或 body 含 project_id)
		switch r.Method {
		case http.MethodGet:
			h.handleList(w, r)
		case http.MethodPost:
			h.handleCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case 1:
		// 单条 /api/outline/{id}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			h.handleGet(w, r, id)
		case http.MethodPut, http.MethodPatch:
			h.handleUpdate(w, r, id)
		case http.MethodDelete:
			h.handleDelete(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.NotFound(w, r)
	}
}

func projectRootFromRequest(r *http.Request) string {
	pr := r.URL.Query().Get("project_root")
	if pr == "" {
		pr = "."
	}
	return pr
}

// projectIDFromRequest 从 query 或 body 解析 project_id.
// 优先 query (?project_id=N), fallback body.
// list 不要求 project_id (项目级列表); create/update 要求 (owner check 后续扩展).
func projectIDFromRequest(r *http.Request, body []byte) int64 {
	if v := r.URL.Query().Get("project_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	var req struct {
		ProjectID int64 `json:"project_id"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	return req.ProjectID
}

// loadOutline 从文件加载, 缺失返空列表.
func loadOutline(projectRoot string) ([]OutlineItem, error) {
	path := filepath.Join(projectRoot, defaultOutlineFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []OutlineItem{}, nil
		}
		return nil, fmt.Errorf("read outline: %w", err)
	}
	var items []OutlineItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse outline: %w", err)
	}
	return items, nil
}

// saveOutline 写盘 (tmp + rename 原子写).
func saveOutline(projectRoot string, items []OutlineItem) error {
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	path := filepath.Join(projectRoot, defaultOutlineFileName)
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// nextOutlineID 返回下一个可用的 ID.
func nextOutlineID(items []OutlineItem) int64 {
	maxID := int64(0)
	for _, item := range items {
		if item.ID > maxID {
			maxID = item.ID
		}
	}
	return maxID + 1
}

func (h *OutlineHandler) handleList(w http.ResponseWriter, r *http.Request) {
	projectRoot := projectRootFromRequest(r)
	outlineStorageMu.RLock()
	items, ok := outlineStorage[projectRoot]
	if !ok {
		// 尝试从文件加载 (跨进程持久化)
		outlineStorageMu.RUnlock()
		loaded, err := loadOutline(projectRoot)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		outlineStorageMu.Lock()
		outlineStorage[projectRoot] = loaded
		outlineStorageMu.Unlock()
		items = loaded
	} else {
		outlineStorageMu.RUnlock()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"outline": items,
		"count":   len(items),
	})
}

func (h *OutlineHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	var item OutlineItem
	if err := json.Unmarshal(body, &item); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if item.Chapter <= 0 {
		http.Error(w, `{"error":"chapter must be > 0"}`, http.StatusBadRequest)
		return
	}
	if item.Title == "" {
		http.Error(w, `{"error":"title required"}`, http.StatusBadRequest)
		return
	}
	if item.Status == "" {
		item.Status = outlineStatusPlanned
	}
	if item.ProjectID == 0 {
		item.ProjectID = projectIDFromRequest(r, body)
	}

	projectRoot := projectRootFromRequest(r)
	outlineStorageMu.Lock()
	items := outlineStorage[projectRoot]
	if len(items) == 0 {
		// 内存无 → 试从文件加载
		loaded, _ := loadOutline(projectRoot)
		items = loaded
	}
	// 检查章节号重复
	for _, existing := range items {
		if existing.Chapter == item.Chapter {
			outlineStorageMu.Unlock()
			http.Error(w, fmt.Sprintf(`{"error":"chapter %d already exists"}`, item.Chapter), http.StatusConflict)
			return
		}
	}
	item.ID = nextOutlineID(items)
	items = append(items, item)
	outlineStorage[projectRoot] = items
	if err := saveOutline(projectRoot, items); err != nil {
		outlineStorageMu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	outlineStorageMu.Unlock()
	respondJSON(w, http.StatusCreated, item)
}

func (h *OutlineHandler) handleGet(w http.ResponseWriter, r *http.Request, id int64) {
	projectRoot := projectRootFromRequest(r)
	outlineStorageMu.RLock()
	items := outlineStorage[projectRoot]
	outlineStorageMu.RUnlock()
	if len(items) == 0 {
		loaded, err := loadOutline(projectRoot)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		outlineStorageMu.Lock()
		outlineStorage[projectRoot] = loaded
		outlineStorageMu.Unlock()
		items = loaded
	}
	for _, item := range items {
		if item.ID == id {
			respondJSON(w, http.StatusOK, item)
			return
		}
	}
	http.Error(w, fmt.Sprintf(`{"error":"outline %d not found"}`, id), http.StatusNotFound)
}

func (h *OutlineHandler) handleUpdate(w http.ResponseWriter, r *http.Request, id int64) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	var input OutlineItem
	if err := json.Unmarshal(body, &input); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if input.Chapter <= 0 {
		http.Error(w, `{"error":"chapter must be > 0"}`, http.StatusBadRequest)
		return
	}
	if input.Title == "" {
		http.Error(w, `{"error":"title required"}`, http.StatusBadRequest)
		return
	}
	input.ID = id // 强制 ID 与 path 一致

	projectRoot := projectRootFromRequest(r)
	outlineStorageMu.Lock()
	items := outlineStorage[projectRoot]
	if len(items) == 0 {
		loaded, _ := loadOutline(projectRoot)
		items = loaded
	}
	found := false
	for i, existing := range items {
		if existing.ID == id {
			input.ID = id
			items[i] = input
			found = true
			break
		}
		// 章节号重复检查 (排除自己)
		if existing.Chapter == input.Chapter && existing.ID != id {
			outlineStorageMu.Unlock()
			http.Error(w, fmt.Sprintf(`{"error":"chapter %d already exists"}`, input.Chapter), http.StatusConflict)
			return
		}
	}
	if !found {
		outlineStorageMu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error":"outline %d not found"}`, id), http.StatusNotFound)
		return
	}
	outlineStorage[projectRoot] = items
	if err := saveOutline(projectRoot, items); err != nil {
		outlineStorageMu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	outlineStorageMu.Unlock()
	respondJSON(w, http.StatusOK, input)
}

func (h *OutlineHandler) handleDelete(w http.ResponseWriter, r *http.Request, id int64) {
	projectRoot := projectRootFromRequest(r)
	outlineStorageMu.Lock()
	items := outlineStorage[projectRoot]
	if len(items) == 0 {
		loaded, _ := loadOutline(projectRoot)
		items = loaded
	}
	newItems := items[:0]
	found := false
	for _, existing := range items {
		if existing.ID == id {
			found = true
			continue
		}
		newItems = append(newItems, existing)
	}
	if !found {
		outlineStorageMu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error":"outline %d not found"}`, id), http.StatusNotFound)
		return
	}
	outlineStorage[projectRoot] = newItems
	if err := saveOutline(projectRoot, newItems); err != nil {
		outlineStorageMu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	outlineStorageMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// ioReadAll 是 net/http.Request.Body 的 ReadAll 包装 (deprecated, 用 io.ReadAll).
// 保留为占位 (历史 compat, 实际由 handle* 改用 io.ReadAll).
var _ = errors.New // 保持 errors import (供其他 future 错误用)
