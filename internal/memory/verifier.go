// Package memory 提供 novel2all-go 长记忆系统.
package memory

// verifier.go 实现一致性检查 Verifier (Sprint 23 + Sprint 30).
//
// 设计:
//   - HasBlockingIssues: 检查 issues 中是否有 critical (阻断写作流)
//   - FilterIssues: 按 category + severity 过滤 issues
//   - Verifier class (Sprint 30): 对齐 Python V0.23+ Verifier.pre_write_check/post_write_check
//
// 参考 Python V0.23 core/memory/verifier.py.
import (
	"context"
	"fmt"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// HasBlockingIssues 检查 issues 是否有 critical.
//
// critical 阻断写作流 (返回 true). warning/info 不阻断 (返回 false).
func HasBlockingIssues(issues []ContinuityIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "critical" {
			return true
		}
	}
	return false
}

// FilterIssues 按 category 和 severity 过滤.
//
//   - categories 为空 → 不过滤
//   - severities 为空 → 不过滤
//   - 都为空 → 返回所有
//
// 返回符合条件的 issues.
func FilterIssues(issues []ContinuityIssue, categories, severities []string) []ContinuityIssue {
	if len(categories) == 0 && len(severities) == 0 {
		return issues
	}
	out := make([]ContinuityIssue, 0, len(issues))
	for _, issue := range issues {
		if !matchSlice(issue.Category, categories) {
			continue
		}
		if !matchSlice(issue.Severity, severities) {
			continue
		}
		out = append(out, issue)
	}
	return out
}

// matchSlice 检查 item 是否在 allowed 列表中. allowed 为空 → true.
func matchSlice(item string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(item, a) {
			return true
		}
	}
	return false
}

// GroupIssuesByCategory 按 category 分组 issues.
func GroupIssuesByCategory(issues []ContinuityIssue) map[string][]ContinuityIssue {
	out := make(map[string][]ContinuityIssue)
	for _, issue := range issues {
		out[issue.Category] = append(out[issue.Category], issue)
	}
	return out
}

// CountIssuesBySeverity 按 severity 统计 issues.
func CountIssuesBySeverity(issues []ContinuityIssue) map[string]int {
	out := make(map[string]int)
	for _, issue := range issues {
		out[issue.Severity]++
	}
	return out
}

// Verifier 一致性检查器 (对齐 Python V0.23+ Verifier class).
type Verifier struct {
	router *llm.Router
}

// NewVerifier 创建 Verifier.
func NewVerifier(router *llm.Router) *Verifier {
	return &Verifier{router: router}
}

// PreWriteCheck 写前一致性检查 (Sprint 30 真实 LLM 调用).
//
// 调用 LLM (CONSISTENCY task) 检查 outline 与现有 state 是否有冲突.
//
// V0.27: router=nil 时返回空 (fail-soft, 不阻断写作流).
func (v *Verifier) PreWriteCheck(ctx context.Context, outline string, state *TrackingState) ([]ContinuityIssue, error) {
	if v.router == nil || outline == "" {
		return nil, nil
	}
	stateText := stateToText(state)
	prompt := fmt.Sprintf("# 写前一致性检查\n\n## 本章大纲\n%s\n\n## 当前状态摘要\n%s\n\n请检查本章大纲是否与历史人物/伏笔/时间线冲突. 返回 JSON: {issues: [{severity, category, description}]}",
		outline, stateText)

	schema := llm.JSONSchema{
		Name: "PreWriteCheckResult",
		Fields: []llm.JSONSchemaField{
			{Name: "issues", Type: "array"},
		},
	}
	target := &preCheckResult{}
	req := llm.Request{
		Task:     llm.TaskConsistency,
		Messages: []llm.Message{{Role: "user", Content: prompt}},
	}
	if err := llm.GenerateJSON(ctx, v.router, req, schema, target); err != nil {
		return nil, nil // fail-soft
	}

	issues := make([]ContinuityIssue, 0, len(target.Issues))
	for _, i := range target.Issues {
		issues = append(issues, ContinuityIssue{
			Severity:    i.Severity,
			Category:    i.Category,
			Description: i.Description,
		})
	}
	return issues, nil
}

// PostWriteCheck 写后一致性检查 (Sprint 30 stub, 真实 LLM 留给 Sprint 27+).
//
// V0.27: 返空 + nil (V0 简化).
func (v *Verifier) PostWriteCheck(ctx context.Context, content string) ([]ContinuityIssue, error) {
	// TODO Sprint 27+: 调 LLM 检查 content 与 state 一致性
	return nil, nil
}

// stateToText 序列化 TrackingState 为 LLM prompt 输入.
func stateToText(state *TrackingState) string {
	if state == nil {
		return "(无状态)"
	}
	var sb strings.Builder
	if state.ProjectName != "" {
		fmt.Fprintf(&sb, "项目: %s\n", state.ProjectName)
	}
	if len(state.Characters) > 0 {
		sb.WriteString("\n角色:\n")
		for name, c := range state.Characters {
			fmt.Fprintf(&sb, "  - %s: %s\n", name, c.Location)
		}
	}
	if len(state.Foreshadowing) > 0 {
		sb.WriteString("\n伏笔:\n")
		for _, f := range state.Foreshadowing {
			fmt.Fprintf(&sb, "  - [%s] %s\n", f.Status, f.Description)
		}
	}
	if len(state.Timeline) > 0 {
		sb.WriteString("\n时间线:\n")
		for _, t := range state.Timeline {
			fmt.Fprintf(&sb, "  - %d: %s\n", t.Chapter, t.Event)
		}
	}
	return sb.String()
}
