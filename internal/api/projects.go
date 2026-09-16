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

// ErrNotFound 通用 not-found 错误
var ErrNotFound = errors.New("not found")

// ProjectsHandler 提供 /api/projects CRUD
type ProjectsHandler struct {
	store *ProjectStore
}

// NewProjectsHandler 创建
func NewProjectsHandler() *ProjectsHandler {
	return &ProjectsHandler{store: NewProjectStore()}
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

// list 列出所有项目
func (h *ProjectsHandler) list(w http.ResponseWriter, _ *http.Request) {
	projects := h.store.List()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"projects": projects,
		"count":    len(projects),
	})
}

// create 新建项目
func (h *ProjectsHandler) create(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.store.Create(req.Name, req.Slug, req.Description, req.OwnerID, req.Genre)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(p)
}

// get 取单个
func (h *ProjectsHandler) get(w http.ResponseWriter, _ *http.Request, id int64) {
	p, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(p)
}

// update 更新
func (h *ProjectsHandler) update(w http.ResponseWriter, r *http.Request, id int64) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Genre       string `json:"genre"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	p, err := h.store.Update(id, req.Name, req.Description, req.Genre)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(p)
}

// delete 删除
func (h *ProjectsHandler) delete(w http.ResponseWriter, _ *http.Request, id int64) {
	if err := h.store.Delete(id); err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
