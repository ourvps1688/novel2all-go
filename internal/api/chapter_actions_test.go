package api

import (
	"bytes"
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

// setupActionsTestWithProjects 创建带 ProjectsRepo 的 ChapterActions + 1 个 alice 拥有的项目.
//
// 用于 owner check 隔离测试 (Sprint V1.0.1 P0-B).
func setupActionsTestWithProjects(t *testing.T) (string, *ChapterHandler, ProjectsRepo, int64) {
	t.Helper()
	dir, h := setupActionsTest(t)

	// 创建 alice 的项目 (OwnerID=1)
	projectStore := NewProjectStore()
	proj, err := projectStore.Create("Alice's Novel", "alice-novel", "", 1 /*alice*/, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// 替换 actions 的 projects 仓库
	actions := NewChapterActionsWithProjects(nil, projectStore)
	h.actions = actions
	return dir, h, projectStore, proj.ID
}

// postJSON 发送 JSON request, 自动注入 admin user (Sprint V1.0.1 P0-B: action handler 需要 user).
//
// admin user bypasses owner check (admin sees all).
func postJSON(h http.Handler, path string, body any) *httptest.ResponseRecorder {
	return postJSONAs(h, path, body, &store.User{
		ID:       999,
		Role:     "admin",
		Username: "test-admin",
	})
}

// postJSONAs 发送 JSON request + 注入指定 user context (用于 owner isolation 测试).
func postJSONAs(h http.Handler, path string, body any, user *store.User) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	if user != nil {
		ctx := context.WithValue(req.Context(), userCtxValue, user)
		req = req.WithContext(ctx)
	}
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

// =====================================================================
// Sprint V1.0.1 P0-B chapter actions owner isolation tests
//
// 验证: 5 个 action (expand/rewrite/review/insert/rollback) 在
// bob 调 alice 的项目章节时返回 403, alice 调自己的 → 200, admin → 200.
// =====================================================================

// TestActions_NoUser_Returns401 验证: 无 user context → 401 (P0-B 双层防御).
func TestActions_NoUser_Returns401(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]string{"project_root": dir}

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/chapter/1/expand/", bytes.NewReader(data))
	// 不注入 user context
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 user 应 401, 实际 %d", rec.Code)
	}
}

// TestActions_OwnerIsolation_Expand 验证: expand owner check.
func TestActions_OwnerIsolation_Expand(t *testing.T) {
	dir, h, _, projectID := setupActionsTestWithProjects(t)
	body := map[string]any{
		"project_root": dir,
		"project_id":   projectID,
	}

	// alice (owner) → 200
	rec := postJSONAs(h, "/api/chapter/1/expand/", body, &store.User{ID: 1, Role: "user", Username: "alice"})
	if rec.Code != http.StatusOK {
		t.Errorf("alice expand: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// bob (not owner) → 403
	rec = postJSONAs(h, "/api/chapter/1/expand/", body, &store.User{ID: 2, Role: "user", Username: "bob"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob expand alice: 应 403, 实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// admin → 200
	rec = postJSONAs(h, "/api/chapter/1/expand/", body, &store.User{ID: 999, Role: "admin", Username: "admin"})
	if rec.Code != http.StatusOK {
		t.Errorf("admin expand: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestActions_OwnerIsolation_Rewrite 验证: rewrite owner check.
func TestActions_OwnerIsolation_Rewrite(t *testing.T) {
	dir, h, _, projectID := setupActionsTestWithProjects(t)
	body := map[string]any{
		"project_root": dir,
		"project_id":   projectID,
	}

	// alice → 200
	rec := postJSONAs(h, "/api/chapter/1/rewrite/", body, &store.User{ID: 1, Role: "user", Username: "alice"})
	if rec.Code != http.StatusOK {
		t.Errorf("alice rewrite: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// bob → 403
	rec = postJSONAs(h, "/api/chapter/1/rewrite/", body, &store.User{ID: 2, Role: "user", Username: "bob"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob rewrite alice: 应 403, 实际 %d", rec.Code)
	}
}

// TestActions_OwnerIsolation_Review 验证: review (读操作) 也做 owner check.
func TestActions_OwnerIsolation_Review(t *testing.T) {
	dir, h, _, projectID := setupActionsTestWithProjects(t)
	body := map[string]any{
		"project_root": dir,
		"project_id":   projectID,
	}

	// bob → 403 (review 也是写行为, 不允许越权)
	rec := postJSONAs(h, "/api/chapter/1/review/", body, &store.User{ID: 2, Role: "user", Username: "bob"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob review alice: 应 403, 实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// alice → 200
	rec = postJSONAs(h, "/api/chapter/1/review/", body, &store.User{ID: 1, Role: "user", Username: "alice"})
	if rec.Code != http.StatusOK {
		t.Errorf("alice review: status=%d", rec.Code)
	}
}

// TestActions_OwnerIsolation_Insert 验证: insert owner check.
func TestActions_OwnerIsolation_Insert(t *testing.T) {
	dir, h, _, projectID := setupActionsTestWithProjects(t)
	body := map[string]any{
		"project_root": dir,
		"project_id":   projectID,
		"position":     2,
		"instruction":  "插入测试",
	}

	rec := postJSONAs(h, "/api/chapter/1/insert/", body, &store.User{ID: 2, Role: "user", Username: "bob"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob insert alice: 应 403, 实际 %d", rec.Code)
	}
}

// TestActions_OwnerIsolation_Rollback 验证: rollback owner check.
func TestActions_OwnerIsolation_Rollback(t *testing.T) {
	dir, h, _, projectID := setupActionsTestWithProjects(t)
	alice := &store.User{ID: 1, Role: "user", Username: "alice"}
	bob := &store.User{ID: 2, Role: "user", Username: "bob"}

	// 先 alice expand 创建 backup
	body := map[string]any{"project_root": dir, "project_id": projectID}
	rec := postJSONAs(h, "/api/chapter/1/expand/", body, alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("alice expand 准备 rollback: %d", rec.Code)
	}

	// bob rollback → 403
	rec = postJSONAs(h, "/api/chapter/1/rollback/", body, bob)
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob rollback alice: 应 403, 实际 %d", rec.Code)
	}

	// alice rollback → 200
	rec = postJSONAs(h, "/api/chapter/1/rollback/", body, alice)
	if rec.Code != http.StatusOK {
		t.Errorf("alice rollback: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestActions_OwnerIsolation_InvalidProjectID 验证: project_id 不存在 → 404.
func TestActions_OwnerIsolation_InvalidProjectID(t *testing.T) {
	dir, h, _, _ := setupActionsTestWithProjects(t)
	body := map[string]any{
		"project_root": dir,
		"project_id":   99999, // 不存在
	}

	rec := postJSONAs(h, "/api/chapter/1/expand/", body, &store.User{ID: 1, Role: "user", Username: "alice"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("invalid project_id 应 404, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestActions_LegacyNoProjectID_SkipsOwnerCheck 验证: 不传 project_id 时 legacy skip (直接单元测试兼容).
func TestActions_LegacyNoProjectID_SkipsOwnerCheck(t *testing.T) {
	dir, h := setupActionsTest(t) // 没注入 projects
	body := map[string]string{"project_root": dir}

	// 即使 user 是普通 user, project_id=0 → 跳过 owner check → action 正常执行
	bob := &store.User{ID: 2, Role: "user", Username: "bob"}
	rec := postJSONAs(h, "/api/chapter/1/expand/", body, bob)
	if rec.Code != http.StatusOK {
		t.Errorf("project_id=0 应跳过 owner check, 实际 %d body=%s", rec.Code, rec.Body.String())
	}
}
