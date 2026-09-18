package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
)

// Project 一个小说项目
//
// P1-F 切片使用内存 store；后续 P2 阶段迁移到 SQLite（store/projects 表）。
// 当前重启数据丢失 — 这是 P1-F mock 设计。
type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	OwnerID     int64     `json:"owner_id"`
	Genre       string    `json:"genre,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectStore 进程内项目存储
type ProjectStore struct {
	mu     sync.RWMutex
	next   int64
	data   map[int64]*Project
	bySlug map[string]int64
}

// NewProjectStore 创建内存 store
func NewProjectStore() *ProjectStore {
	return &ProjectStore{
		next:   0,
		data:   make(map[int64]*Project),
		bySlug: make(map[string]int64),
	}
}

// List 列出项目
func (s *ProjectStore) List() []*Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Project, 0, len(s.data))
	for _, p := range s.data {
		// 拷贝，避免外部修改
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// ListByOwner 按 owner_id 过滤 (Sprint V1.0.1 P0-B owner filter).
//
// 返回所有 OwnerID == ownerID 的项目 (deep copy, 避免外部修改内存).
func (s *ProjectStore) ListByOwner(ownerID int64) []*Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Project, 0)
	for _, p := range s.data {
		if p.OwnerID != ownerID {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// Get 取单个
func (s *ProjectStore) Get(id int64) (*Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

// Create 创建
func (s *ProjectStore) Create(name, slug, desc string, ownerID int64, genre string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		return nil, errors.New("name is required")
	}
	if slug == "" {
		return nil, errors.New("slug is required")
	}
	if _, exists := s.bySlug[slug]; exists {
		return nil, fmt.Errorf("slug %q already exists", slug)
	}
	id := atomic.AddInt64(&s.next, 1)
	now := time.Now()
	p := &Project{
		ID:          id,
		Name:        name,
		Slug:        slug,
		Description: desc,
		OwnerID:     ownerID,
		Genre:       genre,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.data[id] = p
	s.bySlug[slug] = id
	cp := *p
	return &cp, nil
}

// Update 更新
func (s *ProjectStore) Update(id int64, name, desc, genre string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[id]
	if !ok {
		return nil, ErrNotFound
	}
	if name != "" {
		p.Name = name
	}
	if desc != "" {
		p.Description = desc
	}
	if genre != "" {
		p.Genre = genre
	}
	p.UpdatedAt = time.Now()
	cp := *p
	return &cp, nil
}

// Delete 删除
func (s *ProjectStore) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.bySlug, p.Slug)
	delete(s.data, id)
	return nil
}

// RestoreAll 用 projects 列表替换内存数据（P1-F 切片 10 state load 用）
//
// 行为：
//   - projects == nil 或 len == 0 → 清空内存（等价 reset）
//   - 否则 → 清空后重新插入 + 重建 next id 计数器
//
// 线程安全：持有 write lock。
func (s *ProjectStore) RestoreAll(projects []*Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 清空现有数据
	s.data = make(map[int64]*Project)
	s.bySlug = make(map[string]int64)
	s.next = 0

	for _, p := range projects {
		if p == nil {
			continue
		}
		cp := *p // 拷贝避免外部修改影响内存
		s.data[cp.ID] = &cp
		s.bySlug[cp.Slug] = cp.ID
		if cp.ID > s.next {
			s.next = cp.ID
		}
	}
	return nil
}

// ErrNotFound 通用 not-found 错误
var ErrNotFound = errors.New("not found")

// ProjectsHandler 提供 /api/projects CRUD
type ProjectsHandler struct {
	repo ProjectsRepo
}

// NewProjectsHandler 创建（内存版 ProjectsRepo，向后兼容）
func NewProjectsHandler() *ProjectsHandler {
	return &ProjectsHandler{repo: NewProjectStore()}
}

// NewProjectsHandlerWithRepo 用已有 repo 创建（P1-F 切片 10 state 持久化用 + Sprint 15 SQLite）
//
// 让 main.go 创建共享 ProjectsRepo 实例给 ProjectsHandler 和 StatePersistor。
func NewProjectsHandlerWithRepo(repo ProjectsRepo) *ProjectsHandler {
	return &ProjectsHandler{repo: repo}
}

// NewProjectsHandlerWithStore 向后兼容 wrapper（DEPRECATED: 用 NewProjectsHandlerWithRepo）
//
// P1-F 切片 10-12 调用此方法，签名不变。
func NewProjectsHandlerWithStore(store *ProjectStore) *ProjectsHandler {
	return &ProjectsHandler{repo: store}
}

// ServeHTTP 路由分发
//
//	GET    /api/projects         → 列表
//	POST   /api/projects         → 创建
//	GET    /api/projects/{id}    → 取单个
//	PUT    /api/projects/{id}    → 更新
//	DELETE /api/projects/{id}    → 删除
func (h *ProjectsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/projects")
	path = strings.Trim(path, "/")

	if path == "" {
		// 列表 / 创建
		switch r.Method {
		case http.MethodGet:
			h.list(w, r)
		case http.MethodPost:
			h.create(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// 解析 id
	id, err := parseInt64(path)
	if err != nil {
		http.Error(w, `{"error":"invalid project id"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.get(w, r, id)
	case http.MethodPut:
		h.update(w, r, id)
	case http.MethodDelete:
		h.delete(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// list 列出项目 — admin 看全部, 普通 user 只看自己 (Sprint V1.0.1 P0-B owner filter).
//
// 要求 context 中有 *store.User (由 mux-level RequireAuth 注入).
// 无 user → 401 (handler 单元测试需自己注入 user context).
func (h *ProjectsHandler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var projects []*Project
	if auth.IsAdmin(user) {
		projects = h.repo.List()
	} else {
		projects = h.repo.ListByOwner(user.ID)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"projects": projects,
		"count":    len(projects),
	})
}

// create 新建项目 — Sprint V1.0.1 P0-B: owner_id 从 context user 注入, 忽略 request body 的 owner_id.
func (h *ProjectsHandler) create(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		OwnerID     int64  `json:"owner_id"`
		Genre       string `json:"genre"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	// 安全: 即使 request body 含 owner_id, 也用 context user 的 ID
	// 防止恶意 user A 创建项目假装属于 user B (P0-B 防越权)
	p, err := h.repo.Create(req.Name, req.Slug, req.Description, user.ID, req.Genre)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(p)
}

// get 取单个 — Sprint V1.0.1 P0-B: owner check (admin bypass)
func (h *ProjectsHandler) get(w http.ResponseWriter, r *http.Request, id int64) {
	p, ok := h.loadOwnedProject(w, r, id)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(p)
}

// update 更新 — Sprint V1.0.1 P0-B: owner check (admin bypass)
func (h *ProjectsHandler) update(w http.ResponseWriter, r *http.Request, id int64) {
	if _, ok := h.loadOwnedProject(w, r, id); !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Genre       string `json:"genre"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	updated, err := h.repo.Update(id, req.Name, req.Description, req.Genre)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(updated)
}

// delete 删除 — Sprint V1.0.1 P0-B: owner check (admin bypass)
func (h *ProjectsHandler) delete(w http.ResponseWriter, r *http.Request, id int64) {
	if _, ok := h.loadOwnedProject(w, r, id); !ok {
		return
	}
	if err := h.repo.Delete(id); err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadOwnedProject 加载 project 并验证 user 访问权 (Sprint V1.0.1 P0-B helper).
//
// 行为:
//   - 无 user in context → 401 + (nil, false)
//   - project 不存在 → 404 + (nil, false)
//   - admin → (project, true) (无 OwnerID 检查)
//   - user.ID == project.OwnerID → (project, true)
//   - 其他 → 403 + (nil, false)
//
// 返回 *Project 避免重复 Get (handler 拿到 project 后直接用).
func (h *ProjectsHandler) loadOwnedProject(w http.ResponseWriter, r *http.Request, id int64) (*Project, bool) {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return nil, false
	}
	p, err := h.repo.Get(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return nil, false
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return nil, false
	}
	if auth.IsAdmin(user) || p.OwnerID == user.ID {
		return p, true
	}
	http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	return nil, false
}

// parseInt64 安全解析
func parseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
