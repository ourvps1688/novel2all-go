// Package memory 提供 novel2all-go 长记忆系统.
package memory

// verifier.go 实现一致性检查 Verifier (Sprint 23).
//
// 设计:
//   - HasBlockingIssues: 检查 issues 中是否有 critical (阻断写作流)
//   - FilterIssues: 按 category + severity 过滤 issues
//   - 注: V0 不调 LLM (Python V0.23+ 用 LLM 做 pre/post-write 检查, 留给 Sprint 24+)
//
// 参考 Python V0.23 core/memory/verifier.py.
import "strings"

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
