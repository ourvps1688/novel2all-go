package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectStore_CreateAndGet(t *testing.T) {
	s := NewProjectStore()
	p, err := s.Create("My Novel", "my-novel", "desc", 1, "fantasy")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID != 1 || p.Name != "My Novel" {
		t.Errorf("unexpected project: %+v", p)
	}
	got, err := s.Get(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "My Novel" {
		t.Errorf("got.Name = %q, want My Novel", got.Name)
	}
}

func TestProjectStore_DuplicateSlug(t *testing.T) {
	s := NewProjectStore()
	_, _ = s.Create("A", "dup", "", 1, "")
	_, err := s.Create("B", "dup", "", 1, "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate slug error, got %v", err)
	}
}

func TestProjectStore_UpdateAndDelete(t *testing.T) {
	s := NewProjectStore()
	p, _ := s.Create("A", "a", "", 1, "")
	upd, err := s.Update(p.ID, "A2", "new desc", "scifi")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Name != "A2" || upd.Description != "new desc" || upd.Genre != "scifi" {
		t.Errorf("update failed: %+v", upd)
	}
	if err := s.Delete(p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(p.ID); err == nil {
		t.Error("expected not found after delete")
	}
}

func TestProjectsHandler_ListEmpty(t *testing.T) {
	h := NewProjectsHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["count"].(float64) != 0 {
		t.Errorf("count=%v, want 0", resp["count"])
	}
}

func TestProjectsHandler_CreateAndGet(t *testing.T) {
	h := NewProjectsHandler()

	body, _ := json.Marshal(map[string]string{
		"name":  "Test Novel",
		"slug":  "test-novel",
		"genre": "fantasy",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	var p Project
	_ = json.Unmarshal(rec.Body.Bytes(), &p)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/projects/1/", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d", rec.Code)
	}
	var got Project
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "Test Novel" {
		t.Errorf("got name=%q", got.Name)
	}
}

func TestProjectsHandler_GetNotFound(t *testing.T) {
	h := NewProjectsHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/999/", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestProjectsHandler_UpdateAndDelete(t *testing.T) {
	h := NewProjectsHandler()

	// Create
	body, _ := json.Marshal(map[string]string{"name": "A", "slug": "a"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	var p Project
	_ = json.Unmarshal(rec.Body.Bytes(), &p)

	// Update
	body, _ = json.Marshal(map[string]string{"name": "A-updated", "genre": "scifi"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/projects/1/", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d", rec.Code)
	}
	var upd Project
	_ = json.Unmarshal(rec.Body.Bytes(), &upd)
	if upd.Name != "A-updated" || upd.Genre != "scifi" {
		t.Errorf("update failed: %+v", upd)
	}

	// Delete
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/projects/1/", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status=%d, want 204", rec.Code)
	}
}
