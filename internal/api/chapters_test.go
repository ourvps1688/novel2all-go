package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/store"
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

// setupChapterTestWithProjects 创建带 ProjectsRepo 的 ChapterHandler + alice 的项目.
//
// 用于 owner check 隔离测试 (Sprint V1.0.1 P0-B).
func setupChapterTestWithProjects(t *testing.T) (string, *ChapterHandler, ProjectsRepo, int64) {
	t.Helper()
	dir, _ := setupChapterTest(t)

	// 创建 alice 的项目 (OwnerID=1)
	projectStore := NewProjectStore()
	proj, err := projectStore.Create("Alice's Novel", "alice-novel", "", 1 /*alice*/, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// 创建 handler 并注入 projects
	h := NewChapterHandler()
	h.SetProjects(projectStore)
	return dir, h, projectStore, proj.ID
}

// defaultTestUser admin user 用于兼容现有测试 (admin bypasses owner check).
var defaultTestUser = &store.User{ID: 999, Role: "admin", Username: "test-admin"}

// chapterReqAsUser 构造带 user context 的 request (用于直接 handler 测试).
func chapterReqAsUser(method, url string, body []byte, user *store.User) *http.Request {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, url, bodyReader)
	if user != nil {
		ctx := context.WithValue(req.Context(), userCtxValue, user)
		req = req.WithContext(ctx)
	}
	return req
}

// saveChapterAs 发送 POST save request (P0-B: 自动注入 admin user).
func saveChapterAs(t *testing.T, h http.Handler, chapter int, dir, content string, user *store.User) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"content":      content,
		"project_root": dir,
	})
	req := chapterReqAsUser(http.MethodPost, "/api/chapter/"+strconv.Itoa(chapter)+"/save/", body, user)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// exportChapterAs 发送 GET export request (P0-B: 自动注入 admin user).
func exportChapterAs(t *testing.T, h http.Handler, chapter int, dir, format string, user *store.User) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/chapter/" + strconv.Itoa(chapter) + "/export/?project_root=" + dir
	if format != "" {
		url += "&format=" + format
	}
	req := chapterReqAsUser(http.MethodGet, url, nil, user)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
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
	rec := saveChapterAs(t, h, 3, dir, "新内容", defaultTestUser)
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
	rec := exportChapterAs(t, h, 1, dir, "md", defaultTestUser)
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
	rec := exportChapterAs(t, h, 2, dir, "txt", defaultTestUser)
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
	rec := exportChapterAs(t, h, 1, dir, "pdf", defaultTestUser)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

// =====================================================================
// Sprint V1.0.1 P0-B chapters save/export owner isolation tests
//
// 验证: alice 创建项目 + 章节, bob 不能 save/export alice 的章节,
// admin 可跨 user 操作.
// =====================================================================

// TestChapterHandler_Save_NoUser_Returns401 验证: save 无 user → 401.
func TestChapterHandler_Save_NoUser_Returns401(t *testing.T) {
	dir, h := setupChapterTest(t)
	body, _ := json.Marshal(map[string]string{
		"content":      "越权内容",
		"project_root": dir,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chapter/1/save/", bytes.NewReader(body))
	// 不注入 user
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 user 应 401, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestChapterHandler_Export_NoUser_Returns401 验证: export 无 user → 401.
func TestChapterHandler_Export_NoUser_Returns401(t *testing.T) {
	dir, h := setupChapterTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/chapter/1/export/?project_root="+dir, http.NoBody)
	// 不注入 user
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 user 应 401, 实际 %d", rec.Code)
	}
}

// TestChapterHandler_Save_OwnerIsolation 验证: bob 不能 save alice 的项目章节.
func TestChapterHandler_Save_OwnerIsolation(t *testing.T) {
	dir, h, _, projectID := setupChapterTestWithProjects(t)
	alice := &store.User{ID: 1, Role: "user", Username: "alice"}
	bob := &store.User{ID: 2, Role: "user", Username: "bob"}
	admin := &store.User{ID: 999, Role: "admin", Username: "admin"}

	// alice save → 200
	body, _ := json.Marshal(map[string]any{
		"content":      "alice 内容",
		"project_root": dir,
		"project_id":   projectID,
	})
	req := chapterReqAsUser(http.MethodPost, "/api/chapter/1/save/", body, alice)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("alice save: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// bob save alice 的项目 → 403
	body, _ = json.Marshal(map[string]any{
		"content":      "越权内容",
		"project_root": dir,
		"project_id":   projectID,
	})
	req = chapterReqAsUser(http.MethodPost, "/api/chapter/1/save/", body, bob)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob save alice: 应 403, 实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 验证未被 bob 覆盖 (文件仍为 alice 内容)
	data, _ := os.ReadFile(filepath.Join(dir, "prose", "第001章.md"))
	if string(data) != "alice 内容" {
		t.Errorf("文件被覆盖: %q", string(data))
	}

	// admin save → 200 (admin bypass)
	body, _ = json.Marshal(map[string]any{
		"content":      "admin 覆盖",
		"project_root": dir,
		"project_id":   projectID,
	})
	req = chapterReqAsUser(http.MethodPost, "/api/chapter/1/save/", body, admin)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("admin save: 应 200, 实际 %d", rec.Code)
	}
}

// TestChapterHandler_Export_OwnerIsolation 验证: bob 不能 export alice 的项目章节.
func TestChapterHandler_Export_OwnerIsolation(t *testing.T) {
	dir, h, _, projectID := setupChapterTestWithProjects(t)
	alice := &store.User{ID: 1, Role: "user", Username: "alice"}
	bob := &store.User{ID: 2, Role: "user", Username: "bob"}

	// alice export → 200
	req := chapterReqAsUser(http.MethodGet, "/api/chapter/1/export/?project_root="+dir+"&format=md&project_id="+strconv.FormatInt(projectID, 10), nil, alice)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("alice export: 应 200, 实际 %d", rec.Code)
	}

	// bob export alice 的 → 403
	req = chapterReqAsUser(http.MethodGet, "/api/chapter/1/export/?project_root="+dir+"&format=md&project_id="+strconv.FormatInt(projectID, 10), nil, bob)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob export alice: 应 403, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestChapterHandler_Save_LegacyNoProjectID_SkipsOwnerCheck 验证: save 不传 project_id → legacy skip.
func TestChapterHandler_Save_LegacyNoProjectID_SkipsOwnerCheck(t *testing.T) {
	dir := t.TempDir()
	prose := filepath.Join(dir, "prose")
	if err := os.MkdirAll(prose, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	h := NewChapterHandler() // 不注入 projects
	bob := &store.User{ID: 2, Role: "user", Username: "bob"}

	body, _ := json.Marshal(map[string]string{
		"content":      "bob 内容",
		"project_root": dir,
	})
	req := chapterReqAsUser(http.MethodPost, "/api/chapter/1/save/", body, bob)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("project_id=0 + 无 projects 应跳过 owner check, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestChapterHandler_Save_InvalidProjectID 验证: project_id 不存在 → 404.
func TestChapterHandler_Save_InvalidProjectID(t *testing.T) {
	dir, h, _, _ := setupChapterTestWithProjects(t)
	alice := &store.User{ID: 1, Role: "user", Username: "alice"}

	body, _ := json.Marshal(map[string]any{
		"content":      "alice",
		"project_root": dir,
		"project_id":   99999, // 不存在
	})
	req := chapterReqAsUser(http.MethodPost, "/api/chapter/1/save/", body, alice)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("invalid project_id 应 404, 实际 %d body=%s", rec.Code, rec.Body.String())
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
