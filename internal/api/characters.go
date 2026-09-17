package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Character 角色
type Character struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"` // protagonist | supporting | antagonist | minor
	Description  string   `json:"description,omitempty"`
	FirstChapter int      `json:"first_chapter,omitempty"`
	LastChapter  int      `json:"last_chapter,omitempty"`
	Traits       []string `json:"traits,omitempty"`
}

// Relationship 角色关系
type Relationship struct {
	ID          int    `json:"id"`
	CharacterA  string `json:"character_a"` // name (not id)
	CharacterB  string `json:"character_b"`
	Type        string `json:"type"` // friend | enemy | lover | family | mentor | rival
	Description string `json:"description,omitempty"`
}

// Foreshadow 伏笔
type Foreshadow struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	PlantedChapter int    `json:"planted_chapter"`
	PayoffChapter  int    `json:"payoff_chapter,omitempty"`
	Status         string `json:"status"` // active | paid_off | abandoned
	Description    string `json:"description,omitempty"`
}

// CharactersHandler 提供 /api/characters + /api/relationships + /api/foreshadows
//
// P1-F 切片 6：角色/关系/伏笔管理（JSON 文件持久化）
// 端点：
//
//	GET    /api/characters/         → 列出所有角色
//	POST   /api/characters/         → 创建角色
//	GET    /api/relationships/      → 列出所有关系
//	POST   /api/relationships/      → 创建关系
//	GET    /api/foreshadows/        → 列出所有伏笔
type CharactersHandler struct {
	mu    sync.RWMutex
	state map[string]*characterCategory // "characters" | "relationships" | "foreshadows" → state
}

type characterCategory struct {
	mu      sync.RWMutex
	counter int64
	data    map[int]any
}

// NewCharactersHandler 创建
func NewCharactersHandler() *CharactersHandler {
	h := &CharactersHandler{
		state: make(map[string]*characterCategory),
	}
	return h
}

// getCategory 懒加载类别
func (h *CharactersHandler) getCategory(name string) *characterCategory {
	h.mu.RLock()
	c, ok := h.state[name]
	h.mu.RUnlock()
	if ok {
		return c
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.state[name]; ok {
		return c
	}
	h.state[name] = &characterCategory{
		data: make(map[int]any),
	}
	return h.state[name]
}

// ServeHTTP 路由分发
func (h *CharactersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	category := parts[0]

	switch category {
	case "characters", "relationships", "foreshadows":
		// proceed
	default:
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleList(w, r, category)
	case http.MethodPost:
		h.handleCreate(w, r, category)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// CharacterStore 跨类别持久化（JSON 文件）
type CharacterStore struct {
	projectRoot string
}

// LoadCharacters 从文件加载角色列表
func LoadCharacters(projectRoot string) ([]Character, error) {
	return loadCategoryJSON[Character](projectRoot, "characters")
}

// SaveCharacters 保存角色列表到文件
func SaveCharacters(projectRoot string, chars []Character) error {
	return saveCategoryJSON(projectRoot, "characters", chars)
}

// LoadRelationships 从文件加载关系列表
func LoadRelationships(projectRoot string) ([]Relationship, error) {
	return loadCategoryJSON[Relationship](projectRoot, "relationships")
}

// SaveRelationships 保存关系列表到文件
func SaveRelationships(projectRoot string, rels []Relationship) error {
	return saveCategoryJSON(projectRoot, "relationships", rels)
}

// LoadForeshadows 从文件加载伏笔列表
func LoadForeshadows(projectRoot string) ([]Foreshadow, error) {
	return loadCategoryJSON[Foreshadow](projectRoot, "foreshadows")
}

// SaveForeshadows 保存伏笔列表到文件
func SaveForeshadows(projectRoot string, fs []Foreshadow) error {
	return saveCategoryJSON(projectRoot, "foreshadows", fs)
}

// categoryStateDir 返回 state 子目录
func categoryStateDir(projectRoot string) string {
	return filepath.Join(projectRoot, "state")
}

// loadCategoryJSON 通用 JSON 加载
func loadCategoryJSON[T any](projectRoot, name string) ([]T, error) {
	dir := categoryStateDir(projectRoot)
	path := filepath.Join(dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil // 文件不存在返回空列表
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return items, nil
}

// saveCategoryJSON 通用 JSON 保存
func saveCategoryJSON(projectRoot, name string, v any) error {
	dir := categoryStateDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".json")
	return os.WriteFile(path, data, 0o644)
}

func (h *CharactersHandler) handleList(w http.ResponseWriter, r *http.Request, category string) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	var (
		data []byte
		err  error
	)
	switch category {
	case "characters":
		items, e := LoadCharacters(projectRoot)
		err = e
		if e == nil {
			data, _ = json.Marshal(map[string]any{category: items, "count": len(items)})
		}
	case "relationships":
		items, e := LoadRelationships(projectRoot)
		err = e
		if e == nil {
			data, _ = json.Marshal(map[string]any{category: items, "count": len(items)})
		}
	case "foreshadows":
		items, e := LoadForeshadows(projectRoot)
		err = e
		if e == nil {
			data, _ = json.Marshal(map[string]any{category: items, "count": len(items)})
		}
	}
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}

func (h *CharactersHandler) handleCreate(w http.ResponseWriter, r *http.Request, category string) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}

	body, err := readBody(r)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}

	switch category {
	case "characters":
		var c Character
		if err := json.Unmarshal(body, &c); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		if c.Name == "" {
			http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
			return
		}
		if c.Role == "" {
			c.Role = "supporting"
		}
		// 分配 ID
		items, _ := LoadCharacters(projectRoot)
		c.ID = nextID(items, func(c Character) int { return c.ID })
		if err := SaveCharacters(projectRoot, append(items, c)); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusCreated, c)

	case "relationships":
		var rel Relationship
		if err := json.Unmarshal(body, &rel); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		if rel.CharacterA == "" || rel.CharacterB == "" {
			http.Error(w, `{"error":"character_a and character_b required"}`, http.StatusBadRequest)
			return
		}
		if rel.Type == "" {
			rel.Type = "friend"
		}
		items, _ := LoadRelationships(projectRoot)
		rel.ID = nextID(items, func(r Relationship) int { return r.ID })
		if err := SaveRelationships(projectRoot, append(items, rel)); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusCreated, rel)

	case "foreshadows":
		var f Foreshadow
		if err := json.Unmarshal(body, &f); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		if f.Title == "" || f.PlantedChapter <= 0 {
			http.Error(w, `{"error":"title and planted_chapter required"}`, http.StatusBadRequest)
			return
		}
		if f.Status == "" {
			f.Status = "active"
		}
		items, _ := LoadForeshadows(projectRoot)
		f.ID = nextID(items, func(f Foreshadow) int { return f.ID })
		if err := SaveForeshadows(projectRoot, append(items, f)); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusCreated, f)
	}
}

// nextID 返回 items 中最大 ID + 1
func nextID[T any](items []T, idFn func(T) int) int {
	max := 0
	for _, it := range items {
		if id := idFn(it); id > max {
			max = id
		}
	}
	return max + 1
}

// readBody 读取并返回 body bytes
func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, errors.New("empty body")
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

// respondJSON 写 JSON 响应
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
