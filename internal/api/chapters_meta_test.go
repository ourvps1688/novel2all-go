package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// openChapterTestDB 创建测试用 SQLite DB
func openChapterTestDB(t *testing.T) *store.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "chapters.db")
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// TestChapterHandler_SetMetaStore 检查 SetMetaStore 接受 nil / 非 nil
func TestChapterHandler_SetMetaStore(t *testing.T) {
	h := NewChapterHandler()
	h.SetMetaStore(nil) // nil should be no-op safe
	h.SetMetaStore(nil) // 再次 nil
	// 不 panic 即通过
}

// TestChapterHandler_ListFromFS_Fallback 当 meta=nil 时走 filesystem
func TestChapterHandler_ListFromFS_Fallback(t *testing.T) {
	// 临时项目目录
	dir := t.TempDir()
	proseDir := filepath.Join(dir, "prose")
	if err := os.MkdirAll(proseDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 写入 2 个章节
	for i := 1; i <= 2; i++ {
		path := filepath.Join(proseDir, "第001章.md")
		if i == 2 {
			path = filepath.Join(proseDir, "第002章.md")
		}
		content := "# Chapter Title\nThis is the content of chapter " + string(rune('0'+i))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// meta=nil, 应该走 filesystem
	h := NewChapterHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chapters?project_root="+dir, http.NoBody)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Chapters []ChapterInfo `json:"chapters"`
		Count    int           `json:"count"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("expected 2 chapters, got %d", resp.Count)
	}
}

// TestChapterHandler_ListFromMeta 当 meta 注入时走 SQLite
func TestChapterHandler_ListFromMeta(t *testing.T) {
	ctx := context.Background()
	db := openChapterTestDB(t)
	projects := store.NewProjectsStore(db)
	chapters := store.NewChaptersStore(db)

	// 先创建 project（满足 FK）
	_, _ = db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "u", "x")
	p, _ := projects.Create(ctx, "Test", "test", "", "", 1)

	// 插入 3 个 chapter metadata
	for i := 1; i <= 3; i++ {
		_, err := chapters.Upsert(ctx, p.ID, i, "Title "+string(rune('A'+i-1)), "prose/第001章.md", 100*i)
		if err != nil {
			t.Fatalf("Upsert %d: %v", i, err)
		}
	}

	// 注入 meta 后 list 用 SQLite
	h := NewChapterHandler()
	h.SetMetaStore(chapters)

	rr := httptest.NewRecorder()
	url := "/api/chapters?project_id=1"
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Chapters []ChapterInfo `json:"chapters"`
		Count    int           `json:"count"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 3 {
		t.Errorf("expected 3 chapters from meta, got %d", resp.Count)
	}
}

// TestChapterHandler_Save_UpdatesMeta save 时同步写 SQLite metadata
func TestChapterHandler_Save_UpdatesMeta(t *testing.T) {
	ctx := context.Background()
	db := openChapterTestDB(t)
	projects := store.NewProjectsStore(db)
	chapters := store.NewChaptersStore(db)

	_, _ = db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "u", "x")
	p, _ := projects.Create(ctx, "Test", "test", "", "", 1)

	dir := t.TempDir()
	h := NewChapterHandler()
	h.SetMetaStore(chapters)

	// save chapter 1
	bodyStr := `{"content":"# Title\n\nContent body", "project_root":"` + strings.ReplaceAll(dir, "\\", "\\\\") + `", "project_id":1, "title":"Chapter 1"}`
	body := strings.NewReader(bodyStr)
	req := httptest.NewRequest(http.MethodPost, "/api/chapter/1/save", body)
	// P0-B: save handler 现在需要 user context (checkProjectAccess)
	reqCtx := context.WithValue(req.Context(), userCtxValue, &store.User{ID: 1, Username: "u", Role: "admin"})
	req = req.WithContext(reqCtx)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("save expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// filesystem 应该写文件
	filesystemPath := filepath.Join(dir, "prose", "第001章.md")
	if _, err := os.Stat(filesystemPath); err != nil {
		t.Errorf("filesystem file not written: %v", err)
	}

	// SQLite 应该有 metadata
	c, err := chapters.GetByNumber(ctx, p.ID, 1)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if c.Title != "Chapter 1" {
		t.Errorf("Title = %q (expected Chapter 1)", c.Title)
	}
	if c.CharCount != 21 { // "# Title\n\nContent body" = 21 chars
		t.Errorf("CharCount = %d", c.CharCount)
	}
	if !strings.HasSuffix(c.ContentPath, "第001章.md") {
		t.Errorf("ContentPath = %q", c.ContentPath)
	}
}

// TestChapterHandler_Delete_ClearsMeta delete 时同步删除 SQLite metadata
func TestChapterHandler_Delete_ClearsMeta(t *testing.T) {
	ctx := context.Background()
	db := openChapterTestDB(t)
	projects := store.NewProjectsStore(db)
	chapters := store.NewChaptersStore(db)

	_, _ = db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "u", "x")
	p, _ := projects.Create(ctx, "Test", "test", "", "", 1)
	_, _ = chapters.Upsert(ctx, p.ID, 1, "T", "prose/第001章.md", 100)

	dir := t.TempDir()
	// 写 filesystem
	_ = os.MkdirAll(filepath.Join(dir, "prose"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "prose", "第001章.md"), []byte("content"), 0o644)

	h := NewChapterHandler()
	h.SetMetaStore(chapters)

	req := httptest.NewRequest(http.MethodDelete, "/api/chapter/1?project_root="+dir+"&project_id=1", http.NoBody)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	// SQLite metadata 应该被删除
	if _, err := chapters.GetByNumber(ctx, p.ID, 1); err == nil {
		t.Error("expected SQLite metadata to be deleted")
	}
}

// TestRelChapterPath 验证相对路径计算
func TestRelChapterPath(t *testing.T) {
	rel := relChapterPath("/tmp/p", 1)
	if !strings.HasSuffix(rel, "第001章.md") {
		t.Errorf("rel = %q", rel)
	}
	rel2 := relChapterPath(".", 5)
	if !strings.HasSuffix(rel2, "第005章.md") {
		t.Errorf("rel2 = %q", rel2)
	}
}
