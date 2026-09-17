// Package memory 提供 novel2all-go 长记忆系统.
package memory

// multi_reviewer.go 实现 4-role 并行审稿 (Sprint 24).
//
// 设计:
//   - 4 个 reviewer 角色 (story_outliner / chapter_writer / consistency_checker / story_reviewer)
//   - 并行调用 LLM (V0: sequential, Sprint 25+ 可改 goroutine parallel)
//   - 每个 reviewer 输出 ReviewIssue 列表 + QualityScore
//   - MultiAgentReviewer 汇总生成 ReviewReport
//
// 参考 Python V0.30.6 B3 core/memory/multi_reviewer.py.
import (
	"context"
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// ReviewIssue 单个 reviewer 的问题 (extractor ContinuityIssue + reviewer name).
type ReviewIssue struct {
	ContinuityIssue
	Reviewer string `json:"reviewer"` // story_outliner / chapter_writer / etc.
}

// QualityScore 评分 (每个 reviewer 输出).
type QualityScore struct {
	Overall     float64 `json:"overall"`     // 0-10
	Style       float64 `json:"style"`       // 文风 (0-10)
	Plot        float64 `json:"plot"`        // 情节 (0-10)
	Character   float64 `json:"character"`   // 角色 (0-10)
	Originality float64 `json:"originality"` // 原创性 (0-10)
	Reviewer    string  `json:"reviewer"`
}

// ReviewReport 4 reviewer 汇总报告.
type ReviewReport struct {
	Chapter int            `json:"chapter"`
	Issues  []ReviewIssue  `json:"issues"`
	Scores  []QualityScore `json:"scores"`
	Summary string         `json:"summary"`
}

// MultiAgentReviewer 4 role 并行审稿.
type MultiAgentReviewer struct {
	router *llm.Router
}

// NewMultiAgentReviewer 创建.
func NewMultiAgentReviewer(router *llm.Router) *MultiAgentReviewer {
	return &MultiAgentReviewer{router: router}
}

// ReviewerRoles 4 个审稿角色.
var ReviewerRoles = []string{
	"story_outliner",      // 大纲师: 检查大纲是否合理
	"chapter_writer",      // 章节作者: 检查内容是否充实
	"consistency_checker", // 一致性: 检查人物/伏笔/时间线
	"story_reviewer",      // 审稿: 检查错别字/逻辑漏洞/AI 味
}

// Review 并行审稿 chapter (V0: sequential).
//
// chapter: 已写章节号.
// content: 章节正文.
// state: 当前 tracking state.
//
// 返回 ReviewReport (合并所有 reviewer 的 issues + scores).
// 注: router=nil 时仍返回 stub 数据 (用于测试 + 无 LLM 环境).
func (r *MultiAgentReviewer) Review(ctx context.Context, chapter int, content string, state *TrackingState) (*ReviewReport, error) {
	report := &ReviewReport{Chapter: chapter}

	for _, role := range ReviewerRoles {
		// V0 简化: 每个 role 生成自己的 issues (mock 用 LLM 调, 无 LLM 则 fallback)
		issues, score := r.reviewByRole(ctx, role, chapter, content, state)
		if len(issues) > 0 {
			report.Issues = append(report.Issues, issues...)
		}
		if score != nil {
			report.Scores = append(report.Scores, *score)
		}
	}

	report.Summary = summarizeReport(report)
	return report, nil
}

// reviewByRole 单个 role 审稿 (V0 简化: 返回固定 issues + score).
func (r *MultiAgentReviewer) reviewByRole(ctx context.Context, role string, chapter int, content string, state *TrackingState) ([]ReviewIssue, *QualityScore) {
	// V0 stub: 根据角色返回 mock 数据
	// Sprint 25+ 接 LLM prompt (per-role review_prompt)
	switch role {
	case "story_outliner":
		return nil, &QualityScore{Overall: 8.0, Style: 8.5, Plot: 8.0, Reviewer: role}
	case "chapter_writer":
		return nil, &QualityScore{Overall: 7.5, Style: 7.0, Plot: 7.5, Character: 8.0, Reviewer: role}
	case "consistency_checker":
		return r.checkConsistency(ctx, role, content, state), &QualityScore{Overall: 9.0, Style: 9.0, Plot: 9.0, Reviewer: role}
	case "story_reviewer":
		return nil, &QualityScore{Overall: 8.0, Style: 8.0, Originality: 8.0, Reviewer: role}
	}
	return nil, nil
}

// checkConsistency 一致性检查 (V0 简化: 不调 LLM, 仅检查 state 中已知冲突).
func (r *MultiAgentReviewer) checkConsistency(_ context.Context, role, _ string, state *TrackingState) []ReviewIssue {
	if state == nil {
		return nil
	}
	var issues []ReviewIssue
	// 检查 dead character 是否在 recent timeline 出现
	for name, char := range state.Characters {
		if char.Motivation == "dead" {
			// V0 简化: 假设 alive=false 用 motivation="dead" 标识
			// (Python 用 separate death_chapter field)
			issue := ReviewIssue{
				ContinuityIssue: ContinuityIssue{
					Severity:    "warning",
					Category:    "character",
					Description: fmt.Sprintf("角色 %s 已标记为 dead, 检查是否仍在出现", name),
				},
				Reviewer: role,
			}
			issues = append(issues, issue)
		}
	}
	return issues
}

// summarizeReport 生成报告摘要.
func summarizeReport(report *ReviewReport) string {
	if len(report.Issues) == 0 && len(report.Scores) == 0 {
		return "no issues found"
	}
	avgScore := 0.0
	for _, s := range report.Scores {
		avgScore += s.Overall
	}
	if len(report.Scores) > 0 {
		avgScore /= float64(len(report.Scores))
	}
	return fmt.Sprintf("%d reviewers, %d issues, avg score %.1f",
		len(report.Scores), len(report.Issues), avgScore)
}
