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

// Review 并行审稿 chapter (Sprint 26: 真实 LLM 并行).
//
// chapter: 已写章节号.
// content: 章节正文.
// state: 当前 tracking state.
//
// 返回 ReviewReport (合并所有 reviewer 的 issues + scores).
// router=nil 时返 stub (测试 + 无 LLM 环境).
func (r *MultiAgentReviewer) Review(ctx context.Context, chapter int, content string, state *TrackingState) (*ReviewReport, error) {
	report := &ReviewReport{Chapter: chapter}

	if r.router == nil {
		// 无 LLM: 顺序执行 stub 模式
		for _, role := range ReviewerRoles {
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

	// LLM 模式: 4 reviewer goroutine 并行
	type result struct {
		issues []ReviewIssue
		score  *QualityScore
		role   string
	}
	results := make(chan result, len(ReviewerRoles))

	for _, role := range ReviewerRoles {
		go func(role string) {
			issues, score := r.reviewByRole(ctx, role, chapter, content, state)
			results <- result{issues: issues, score: score, role: role}
		}(role)
	}

	for i := 0; i < len(ReviewerRoles); i++ {
		res := <-results
		if len(res.issues) > 0 {
			report.Issues = append(report.Issues, res.issues...)
		}
		if res.score != nil {
			report.Scores = append(report.Scores, *res.score)
		}
	}

	report.Summary = summarizeReport(report)
	return report, nil
}

// reviewByRole 单个 role 审稿 (Sprint 26: router 可用时调 LLM).
func (r *MultiAgentReviewer) reviewByRole(ctx context.Context, role string, chapter int, content string, state *TrackingState) ([]ReviewIssue, *QualityScore) {
	if r.router == nil {
		// Stub mode: 返回固定 score (Sprint 24 行为, 兼容旧测试)
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

	// LLM mode: 调 router 生成 review
	schema := llm.JSONSchema{
		Name:        "RoleReview",
		Description: role + " reviewer 输出",
		Fields: []llm.JSONSchemaField{
			{Name: "issues", Type: "array"},
			{Name: "scores", Type: "object"},
		},
	}

	prompt := buildRoleReviewPrompt(role, chapter, content, state)
	req := llm.Request{
		Task:     llm.TaskConsistency,
		Messages: []llm.Message{{Role: "user", Content: prompt}},
	}

	target := &roleReviewResult{}
	if err := llm.GenerateJSON(ctx, r.router, req, schema, target); err != nil {
		return nil, &QualityScore{Overall: 7.0, Reviewer: role} // fail-soft
	}

	// 转换
	score := &QualityScore{
		Overall:     target.Scores.Overall,
		Style:       target.Scores.Style,
		Plot:        target.Scores.Plot,
		Character:   target.Scores.Character,
		Originality: target.Scores.Originality,
		Reviewer:    role,
	}
	if score.Overall == 0 {
		score.Overall = 7.0
	}

	issues := make([]ReviewIssue, 0, len(target.Issues))
	for _, iss := range target.Issues {
		issues = append(issues, ReviewIssue{
			ContinuityIssue: ContinuityIssue{
				Severity:    iss.Severity,
				Category:    iss.Category,
				Description: iss.Description,
			},
			Reviewer: role,
		})
	}
	return issues, score
}

// roleReviewResult LLM 输出 schema.
type roleReviewResult struct {
	Issues []struct {
		Severity    string `json:"severity"`
		Category    string `json:"category"`
		Description string `json:"description"`
	} `json:"issues"`
	Scores struct {
		Overall     float64 `json:"overall"`
		Style       float64 `json:"style"`
		Plot        float64 `json:"plot"`
		Character   float64 `json:"character"`
		Originality float64 `json:"originality"`
	} `json:"scores"`
}

// buildRoleReviewPrompt 构造 per-role prompt.
func buildRoleReviewPrompt(role string, chapter int, content string, state *TrackingState) string {
	prompts := map[string]string{
		"story_outliner":      "你是大纲师. 检查本章大纲合理性: 情节推进是否合理, 是否有逻辑漏洞.",
		"chapter_writer":      "你是章节作者. 检查本章内容充实度: 场景描写/对话/心理是否到位.",
		"consistency_checker": "你是一致性检查员. 检查人物/伏笔/时间线是否有冲突.",
		"story_reviewer":      "你是审稿. 检查错别字/标点/AI 味/重复表达.",
	}
	base, ok := prompts[role]
	if !ok {
		base = "你是审稿员."
	}
	stateText := stateToText(state)
	return fmt.Sprintf("%s\n\n第 %d 章内容:\n%s\n\n当前状态:\n%s\n\n返回 JSON: {issues: [...], scores: {overall, style, plot, character, originality}}", base, chapter, content, stateText)
}

// StateToText 暴露 stateToText 给外部 (Sprint 30 helper, 对齐 Python _state_to_text).
func StateToText(state *TrackingState) string {
	return stateToText(state)
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
