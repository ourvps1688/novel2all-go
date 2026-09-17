package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func getJSON(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postJSONReq(h http.Handler, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCharactersHandler_CreateAndList(t *testing.T) {
	dir := t.TempDir()
	h := NewCharactersHandler()

	// Create
	body := map[string]any{
		"name":          "林轩",
		"role":          "protagonist",
		"description":   "天才少年",
		"first_chapter": 1,
		"traits":        []string{"勇敢", "聪慧"},
	}
	rec := postJSONReq(h, "/api/characters?project_root="+dir, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var c Character
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	if c.ID == 0 || c.Name != "林轩" {
		t.Errorf("character wrong: %+v", c)
	}

	// List
	rec = getJSON(h, "/api/characters?project_root="+dir)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp struct {
		Characters []Character `json:"characters"`
		Count      int         `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 1 || resp.Characters[0].Name != "林轩" {
		t.Errorf("list wrong: %+v", resp)
	}
}

func TestCharactersHandler_EmptyList(t *testing.T) {
	dir := t.TempDir()
	h := NewCharactersHandler()
	rec := getJSON(h, "/api/characters?project_root="+dir)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	// count 应为 0
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["count"].(float64) != 0 {
		t.Errorf("count=%v, want 0", resp["count"])
	}
}

func TestCharactersHandler_MissingName(t *testing.T) {
	dir := t.TempDir()
	h := NewCharactersHandler()
	rec := postJSONReq(h, "/api/characters?project_root="+dir, map[string]string{"role": "supporting"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestCharactersHandler_Persistence(t *testing.T) {
	dir := t.TempDir()
	h := NewCharactersHandler()

	// Create
	rec := postJSONReq(h, "/api/characters?project_root="+dir, map[string]any{
		"name": "persist_test",
		"role": "minor",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create failed: %s", rec.Body.String())
	}

	// 验证文件已写
	path := filepath.Join(dir, "state", "characters.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not written: %v", err)
	}

	// 新 handler 实例读取（模拟重启）
	h2 := NewCharactersHandler()
	rec2 := getJSON(h2, "/api/characters?project_root="+dir)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status=%d", rec2.Code)
	}
	var resp struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp)
	if resp.Count != 1 {
		t.Errorf("persistence failed: count=%d", resp.Count)
	}
}

func TestRelationshipsHandler_CreateAndList(t *testing.T) {
	dir := t.TempDir()
	h := NewCharactersHandler()
	body := map[string]any{
		"character_a": "Alice",
		"character_b": "Bob",
		"type":        "friend",
		"description": "青梅竹马",
	}
	rec := postJSONReq(h, "/api/relationships?project_root="+dir, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var r Relationship
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.ID == 0 || r.CharacterA != "Alice" {
		t.Errorf("rel wrong: %+v", r)
	}

	rec = getJSON(h, "/api/relationships?project_root="+dir)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp struct {
		Relationships []Relationship `json:"relationships"`
		Count         int            `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 1 {
		t.Errorf("count=%d", resp.Count)
	}
}

func TestForeshadowsHandler_CreateAndList(t *testing.T) {
	dir := t.TempDir()
	h := NewCharactersHandler()
	body := map[string]any{
		"title":           "神秘的钥匙",
		"planted_chapter": 3,
		"description":     "第 3 章主角捡到一把古钥匙",
	}
	rec := postJSONReq(h, "/api/foreshadows?project_root="+dir, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var f Foreshadow
	_ = json.Unmarshal(rec.Body.Bytes(), &f)
	if f.ID == 0 || f.PlantedChapter != 3 {
		t.Errorf("foreshadow wrong: %+v", f)
	}
	if f.Status != "active" {
		t.Errorf("default status should be 'active', got %q", f.Status)
	}

	rec = getJSON(h, "/api/foreshadows?project_root="+dir)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp struct {
		Foreshadows []Foreshadow `json:"foreshadows"`
		Count       int          `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 1 {
		t.Errorf("count=%d", resp.Count)
	}
}

func TestForeshadowsHandler_MissingFields(t *testing.T) {
	dir := t.TempDir()
	h := NewCharactersHandler()
	// 缺 planted_chapter
	rec := postJSONReq(h, "/api/foreshadows?project_root="+dir, map[string]string{"title": "no_chapter"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestCharactersHandler_UnknownCategory(t *testing.T) {
	h := NewCharactersHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/unknown/", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestCharactersHandler_MethodNotAllowed(t *testing.T) {
	h := NewCharactersHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/characters/", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d, want 405", rec.Code)
	}
}

func TestNextID(t *testing.T) {
	type item struct {
		ID int `json:"id"`
	}
	items := []item{{ID: 1}, {ID: 5}, {ID: 3}}
	got := nextID(items, func(i item) int { return i.ID })
	if got != 6 {
		t.Errorf("nextID=%d, want 6", got)
	}
}
