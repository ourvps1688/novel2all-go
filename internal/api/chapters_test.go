package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupChapterTest(t *testing.T) (string, *ChapterHandler) {
	t.Helper()
	dir := t.TempDir()
	prose := filepath.Join(dir, "prose")
	if err := os.MkdirAll(prose, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 写两章
	if err := os.WriteFile(filepath.Join(prose, "第001章.md"), []byte("# 第 1 章\n这是第一章内容。\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prose, "第002章.md"), []byte("# 第 2 章\n**粗体** 内容。\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 写一个非章节文件（应被过滤）
	if err := os.WriteFile(filepath.Join(prose, "readme.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir, NewChapterHandler()
}

func TestChapterHandler_List(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/chapters?project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp struct {
		Chapters []ChapterInfo `json:"chapters"`
		Count    int           `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if resp.Count != 2 {
		t.Errorf("count=%d, want 2", resp.Count)
	}
	if resp.Chapters[0].Chapter != 1 || resp.Chapters[1].Chapter != 2 {
		t.Errorf("chapters order: %+v", resp.Chapters)
	}
}

func TestChapterHandler_List_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	h := NewChapterHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/chapters?project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d", rec.Code)
	}
}

func TestChapterHandler_Content(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/chapter/1/content/?project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var c ChapterContent
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	if c.Chapter != 1 || !strings.Contains(c.Content, "第一章") {
		t.Errorf("content wrong: %+v", c)
	}
}

func TestChapterHandler_Content_NotFound(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/chapter/999/content/?project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestChapterHandler_Save(t *testing.T) {
	dir, h := setupChapterTest(t)
	body, _ := json.Marshal(map[string]string{
		"content":      "新内容",
		"project_root": dir,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chapter/3/save/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 验证文件已写入
	path := filepath.Join(dir, "prose", "第003章.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(data) != "新内容" {
		t.Errorf("file content=%q", string(data))
	}
}

func TestChapterHandler_Delete(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/chapter/1/?project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status=%d, want 204", rec.Code)
	}
	// 验证已删除
	path := filepath.Join(dir, "prose", "第001章.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be deleted")
	}
}

func TestChapterHandler_Delete_NotFound(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/chapter/999/?project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestChapterHandler_Export_MD(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/chapter/1/export/?format=md&project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "markdown") {
		t.Errorf("Content-Type=%q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "chapter_001.md") {
		t.Errorf("Content-Disposition=%q", cd)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "第一章") {
		t.Errorf("body=%q", string(body))
	}
}

func TestChapterHandler_Export_TXT_StripsMarkdown(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/chapter/2/export/?format=txt&project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	// 应该有 # / ** 标记被剥离
	if strings.Contains(string(body), "**") || strings.Contains(string(body), "# 第") {
		t.Errorf("markdown not stripped: %q", string(body))
	}
}

func TestChapterHandler_Export_UnsupportedFormat(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/chapter/1/export/?format=pdf&project_root="+dir, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestStripMarkdown(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"# 标题", "标题"},
		{"**粗体**", "粗体"},
		{"*斜体*", "斜体"},
		{"`code`", "code"},
		{"[link](https://x.com)", "link"},
		{"![alt](img.png)", "alt"},
		{"普通文本", "普通文本"},
	}
	for _, c := range cases {
		got := stripMarkdown(c.in)
		if got != c.want {
			t.Errorf("stripMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
