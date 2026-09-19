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
)

// 类别与状态常量（避免 goconst）
const (
	categoryCharacters    = "characters"
	categoryRelationships = "relationships"
	categoryForeshadows   = "foreshadows"
	statusActive          = "active"
)

// Character 角色
type Character struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Description  string   `json:"description,omitempty"`
	FirstChapter int      `json:"first_chapter,omitempty"`
	LastChapter  int      `json:"last_chapter,omitempty"`
	Traits       []string `json:"traits,omitempty"`
}

// Relationship 角色关系
type Relationship struct {
	ID          int    `json:"id"`
	CharacterA  string `json:"character_a"`
	CharacterB  string `json:"character_b"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// Foreshadow 伏笔
type Foreshadow struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	PlantedChapter int    `json:"planted_chapter"`
	PayoffChapter  int    `json:"payoff_chapter,omitempty"`
	Status         string `json:"status"`
	Description    string `json:"description,omitempty"`
}

// CharactersHandler 提供 /api/characters + /api/relationships + /api/foreshadows
type CharactersHandler struct{}

// NewCharactersHandler 创建
func NewCharactersHandler() *CharactersHandler { return &CharactersHandler{} }

// ServeHTTP 路由分发
func (h *CharactersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch parts[0] {
	case categoryCharacters, categoryRelationships, categoryForeshadows:
	default:
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.handleList(w, r, parts[0])
	case http.MethodPost:
		h.handleCreate(w, r, parts[0])
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *CharactersHandler) handleList(w http.ResponseWriter, r *http.Request, category string) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	items, err := loadCategoryJSON[any](projectRoot, category)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		category: items,
		"count":  len(items),
	})
}

//nolint:gocyclo // 三类别分发天然多分支
func (h *CharactersHandler) handleCreate(w http.ResponseWriter, r *http.Request, category string) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}

	body, err := readBodyAll(r)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}

	switch category {
	case categoryCharacters:
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
		items, _ := loadCategoryJSON[Character](projectRoot, categoryCharacters)
		c.ID = nextID(items, func(c Character) int { return c.ID })
		if err := saveCategoryJSON(projectRoot, categoryCharacters, append(items, c)); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusCreated, c)

	case categoryRelationships:
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
		items, _ := loadCategoryJSON[Relationship](projectRoot, categoryRelationships)
		rel.ID = nextID(items, func(r Relationship) int { return r.ID })
		if err := saveCategoryJSON(projectRoot, categoryRelationships, append(items, rel)); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusCreated, rel)

	case categoryForeshadows:
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
			f.Status = statusActive
		}
		items, _ := loadCategoryJSON[Foreshadow](projectRoot, categoryForeshadows)
		f.ID = nextID(items, func(f Foreshadow) int { return f.ID })
		if err := saveCategoryJSON(projectRoot, categoryForeshadows, append(items, f)); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusCreated, f)
	}
}

// categoryStateDir 返回项目下 state/ 目录
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
			return nil, nil
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
	return os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644)
}

// nextID 返回 items 中最大 ID + 1
func nextID[T any](items []T, idFn func(T) int) int {
	maxID := 0
	for _, it := range items {
		if id := idFn(it); id > maxID {
			maxID = id
		}
	}
	return maxID + 1
}

// readBodyAll 读取 body bytes
func readBodyAll(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, errors.New("empty body")
	}
	defer func() { _ = r.Body.Close() }()
	return io.ReadAll(r.Body)
}

// respondJSON 写 JSON 响应
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
