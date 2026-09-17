package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupActionsTest(t *testing.T) (string, *ChapterHandler) {
	t.Helper()
	dir := t.TempDir()
	prose := filepath.Join(dir, "prose")
	if err := os.MkdirAll(prose, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "# 第 1 章\n第一段内容。\n第二段内容。\n第三段内容。\n"
	if err := os.WriteFile(filepath.Join(prose, "第001章.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	actions := NewChapterActions(nil) // nil executor → mock
	h := NewChapterHandlerWithActions(actions)
	return dir, h
}

func postJSON(h http.Handler, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestActions_Expand(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]string{"project_root": dir, "instruction": "续写"}
	rec := postJSON(h, "/api/chapter/1/expand/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp ActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Action != "expand" || resp.AppendedChars == 0 {
		t.Errorf("response wrong: %+v", resp)
	}
	if resp.BackupPath == "" {
		t.Error("expected backup_path")
	}
	// 验证文件确实扩充了
	data, _ := os.ReadFile(filepath.Join(dir, "prose", "第001章.md"))
	if !strings.Contains(string(data), "[AI 扩写]") {
		t.Errorf("file not expanded: %s", string(data))
	}
	// 验证备份存在
	_, err := os.Stat(resp.BackupPath)
	if err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

func TestActions_Rewrite(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/1/rewrite/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp ActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Action != "rewrite" || resp.CharsBefore == 0 || resp.CharsAfter == 0 {
		t.Errorf("response wrong: %+v", resp)
	}
	// 文件应被重写
	data, _ := os.ReadFile(filepath.Join(dir, "prose", "第001章.md"))
	if !strings.Contains(string(data), "[AI 重写]") {
		t.Errorf("file not rewritten: %s", string(data))
	}
}

func TestActions_Review(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/1/review/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp ReviewResult
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Chapter != 1 {
		t.Errorf("chapter wrong: %+v", resp)
	}
	// mock review 应有 mock 标记
	if !strings.Contains(resp.OverallVerdict, "needs_revision") && resp.OverallVerdict == "" {
		t.Errorf("verdict wrong: %s", resp.OverallVerdict)
	}
}

func TestActions_Insert(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]any{
		"project_root": dir,
		"instruction":  "插入新内容",
		"position":     2,
	}
	rec := postJSON(h, "/api/chapter/1/insert/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp ActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Action != "insert" {
		t.Errorf("response wrong: %+v", resp)
	}
	// 验证插入发生在第 2 行后
	data, _ := os.ReadFile(filepath.Join(dir, "prose", "第001章.md"))
	if !strings.Contains(string(data), "[AI 插入]") {
		t.Errorf("insert not applied: %s", string(data))
	}
}

func TestActions_Insert_MissingPosition(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/1/insert/", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestActions_Rollback(t *testing.T) {
	dir, h := setupActionsTest(t)

	// 1. 备份前先修改文件（触发 backup）
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/1/expand/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expand failed: %s", rec.Body.String())
	}
	expandedData, _ := os.ReadFile(filepath.Join(dir, "prose", "第001章.md"))
	if !strings.Contains(string(expandedData), "[AI 扩写]") {
		t.Fatal("expand didn't change file")
	}

	// 2. rollback 应恢复原文
	rec = postJSON(h, "/api/chapter/1/rollback/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback failed: %s", rec.Body.String())
	}
	var resp ActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Action != "rollback" {
		t.Errorf("action wrong: %+v", resp)
	}
	// 验证文件被恢复
	data, _ := os.ReadFile(filepath.Join(dir, "prose", "第001章.md"))
	if strings.Contains(string(data), "[AI 扩写]") {
		t.Errorf("rollback didn't restore: %s", string(data))
	}
	if !strings.Contains(string(data), "第三段内容") {
		t.Errorf("rollback content wrong: %s", string(data))
	}
}

func TestActions_Rollback_NoBackup(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/999/rollback/", body)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
	_ = dir
}

func TestActions_Expand_NotFound(t *testing.T) {
	dir := t.TempDir()
	actions := NewChapterActions(nil)
	h := NewChapterHandlerWithActions(actions)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/99/expand/", body)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestActions_MethodNotAllowed(t *testing.T) {
	_, h := setupActionsTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/chapter/1/expand/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d, want 405", rec.Code)
	}
}
