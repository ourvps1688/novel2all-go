// Sprint 34c.3: 5 action (expand/rewrite/review/insert/rollback) + verifier post-check 集成测试.
//
// 覆盖:
//   - Verifier 接口 mock 实现 (return 1-3 issues / 22 issues 截断 / panic recover)
//   - 5 action 全跑 (mock executor 路径, V0.30 兼容)
//   - Issues 字段正确序列化到 ActionResponse
//   - 向后兼容: NewChapterActions(nil) 不传 verifier → Issues=nil
//
// 不依赖真 LLM, 纯 HTTP 集成.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubVerifier mock verifier (Sprint 34c 测试用).
//
// preContent 包含特定字符串时返回特定 issues (便于断言).
// alwaysTruncate 启用时永远返回 22 条 issues (验证 <=20 截断).
// panicOn 启用时永远 panic (验证 recover 不影响 action 成功).
type stubVerifier struct {
	preContent     string
	preIssues      []PostCheckIssue
	alwaysTruncate bool
	panicOn        bool
}

func (s *stubVerifier) Check(_ context.Context, content string) []PostCheckIssue {
	if s.panicOn {
		panic("stubVerifier simulated panic")
	}
	if s.preContent != "" && strings.Contains(content, s.preContent) {
		return s.preIssues
	}
	if s.alwaysTruncate {
		out := make([]PostCheckIssue, 22)
		for i := range out {
			out[i] = PostCheckIssue{Severity: "warning", Category: "flood", Message: "fill"}
		}
		return out
	}
	return nil
}

// setupActionsWithVerifier 测试 fixtures (带 verifier 注入).
func setupActionsWithVerifier(t *testing.T, v Verifier) (string, *ChapterHandler) {
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
	actions := NewChapterActionsWithVerifier(nil /* executor=nil mock */, nil /* memMgr=nil */, v)
	h := NewChapterHandlerWithActions(actions)
	return dir, h
}

// TestActions_Expand_WithVerifierIssues 验证 expand action 后 verifier issues 写到 response.
func TestActions_Expand_WithVerifierIssues(t *testing.T) {
	v := &stubVerifier{
		preContent: "[AI 扩写]",
		preIssues: []PostCheckIssue{
			{Severity: "warning", Category: "consistency", Message: "角色名字不一致"},
		},
	}
	dir, h := setupActionsWithVerifier(t, v)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/1/expand/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp ActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(resp.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(resp.Issues), resp.Issues)
	}
	if resp.Issues[0].Category != "consistency" {
		t.Errorf("issue category wrong: %s", resp.Issues[0].Category)
	}
	if resp.Issues[0].Message != "角色名字不一致" {
		t.Errorf("issue message wrong: %s", resp.Issues[0].Message)
	}
}

// TestActions_Rewrite_WithVerifierIssues 验证 rewrite action 后 verifier issues.
func TestActions_Rewrite_WithVerifierIssues(t *testing.T) {
	v := &stubVerifier{
		preContent: "[AI 重写]",
		preIssues: []PostCheckIssue{
			{Severity: "critical", Category: "plot", Message: "主线被改"},
			{Severity: "info", Category: "style", Message: "风格微变"},
		},
	}
	dir, h := setupActionsWithVerifier(t, v)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/1/rewrite/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp ActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %+v", len(resp.Issues), resp.Issues)
	}
	got := map[string]string{}
	for _, is := range resp.Issues {
		got[is.Severity] = is.Message
	}
	if got["critical"] != "主线被改" {
		t.Errorf("critical issue wrong: %s", got["critical"])
	}
	if got["info"] != "风格微变" {
		t.Errorf("info issue wrong: %s", got["info"])
	}
}

// TestActions_Insert_WithVerifierIssues 验证 insert action 后 verifier issues 截断到 <=20.
func TestActions_Insert_WithVerifierIssues(t *testing.T) {
	v := &stubVerifier{alwaysTruncate: true}
	dir, h := setupActionsWithVerifier(t, v)
	body := map[string]any{
		"project_root": dir,
		"position":     2,
		"instruction":  "插入段落",
	}
	rec := postJSON(h, "/api/chapter/1/insert/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp ActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Issues) != 20 {
		t.Errorf("expected issues truncated to 20, got %d", len(resp.Issues))
	}
}

// TestActions_PanicVerifierRecovered verifier panic 应被 recover, action 仍 200.
func TestActions_PanicVerifierRecovered(t *testing.T) {
	v := &stubVerifier{panicOn: true}
	dir, h := setupActionsWithVerifier(t, v)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/1/expand/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("action should succeed despite verifier panic: status=%d body=%s",
			rec.Code, rec.Body.String())
	}
	var resp ActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Issues) != 0 {
		t.Errorf("panic-recovered verifier should return no issues, got %d", len(resp.Issues))
	}
}

// TestActions_NilVerifierBackwardCompat 验证 verifier=nil (V0.30 调用方式) 仍兼容.
func TestActions_NilVerifierBackwardCompat(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]string{"project_root": dir}
	rec := postJSON(h, "/api/chapter/1/expand/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"issues":`) {
		t.Errorf("issues field should be omitted when verifier=nil: %s", rec.Body.String())
	}
}

// TestActions_5ActionsIntegration 5 action 端到端跑 (mock executor + mock verifier).
func TestActions_5ActionsIntegration(t *testing.T) {
	v := &stubVerifier{
		preContent: "[AI",
		preIssues: []PostCheckIssue{
			{Severity: "warning", Category: "consistency", Message: "post-check ok"},
		},
	}
	dir, h := setupActionsWithVerifier(t, v)

	cases := []struct {
		name       string
		path       string
		body       map[string]any
		wantAction string
	}{
		{
			name: "expand", path: "/api/chapter/1/expand/",
			body:       map[string]any{"project_root": dir},
			wantAction: "expand",
		},
		{
			name: "rewrite", path: "/api/chapter/1/rewrite/",
			body:       map[string]any{"project_root": dir},
			wantAction: "rewrite",
		},
		{
			name: "insert", path: "/api/chapter/1/insert/",
			body:       map[string]any{"project_root": dir, "position": 2, "instruction": "插入"},
			wantAction: "insert",
		},
		{
			name: "review", path: "/api/chapter/1/review/",
			body:       map[string]any{"project_root": dir},
			wantAction: "",
		},
		{
			name: "rollback", path: "/api/chapter/1/rollback/",
			body:       map[string]any{"project_root": dir},
			wantAction: "rollback",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(h, tc.path, tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status=%d body=%s", tc.name, rec.Code, rec.Body.String())
			}
			bodyStr := rec.Body.String()
			if tc.wantAction != "" {
				var resp ActionResponse
				_ = json.Unmarshal([]byte(bodyStr), &resp)
				if resp.Action != tc.wantAction {
					t.Errorf("%s action=%q want=%q", tc.name, resp.Action, tc.wantAction)
				}
				if tc.wantAction != "rollback" {
					if !strings.Contains(bodyStr, `"issues"`) {
						t.Errorf("%s expected issues in response, got: %s", tc.name, bodyStr)
					}
				}
			} else {
				var rev ReviewResult
				_ = json.Unmarshal([]byte(bodyStr), &rev)
				if rev.Chapter != 1 {
					t.Errorf("review chapter wrong: %+v", rev)
				}
			}
		})
	}
}

// TestNewChapterActionsWithVerifier_NoMemMgr 验证 NewChapterActionsWithVerifier
// 第三个参数 (verifier) 为 nil 时仍能正常用 (跟现有 NewChapterActions 等价).
func TestNewChapterActionsWithVerifier_NoMemMgr(t *testing.T) {
	a := NewChapterActionsWithVerifier(nil, nil, nil)
	if a == nil {
		t.Fatal("constructor returned nil")
	}
	if a.executor != nil {
		t.Error("executor should be nil")
	}
	if a.memMgr != nil {
		t.Error("memMgr should be nil")
	}
	if a.verifier != nil {
		t.Error("verifier should be nil")
	}
	issues := a.runVerifierCheck(context.Background(), "test")
	if issues != nil {
		t.Errorf("nil verifier should return nil issues, got %v", issues)
	}
}

// TestIssueTruncationBoundary 边界: 22 条 issue 截断到 20 条.
func TestIssueTruncationBoundary(t *testing.T) {
	v := &stubVerifier{alwaysTruncate: true}
	a := NewChapterActionsWithVerifier(nil, nil, v)
	issues := a.runVerifierCheck(context.Background(), "anything")
	if len(issues) != 20 {
		t.Errorf("expected 20 issues (truncated from 22), got %d", len(issues))
	}
}
