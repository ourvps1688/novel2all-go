// Package skills 提供 novel2all-go 的 skill 加载与执行能力。
//
// 13 个 SKILL.md 通过 embed.FS 嵌入二进制，运行时无需文件系统。
// SKILL.md 格式：
//
//	---
//	name: story-x
//	description: "..."
//	---
//
//	# title
//
//	body body（作为 LLM system prompt）
package skills

// Skill 加载后的技能定义
type Skill struct {
	Name        string // 来自 frontmatter name
	Description string // 来自 frontmatter description
	Body        string // frontmatter 之后的 markdown body
	// SourcePath 原始文件名（用于 debug + 日志）
	SourcePath string
}

// ExecuteInput 执行输入
type ExecuteInput struct {
	// SkillName skill 名（不含 .md）
	SkillName string

	// UserInput 用户输入（作为 user message）
	UserInput string

	// Variables 模板变量（用于未来扩展，V0.1 暂未使用）
	Variables map[string]any
}

// ExecuteResult 执行结果（非流式）
type ExecuteResult struct {
	Content   string
	Provider  string
	Model     string
	TokensIn  int
	TokensOut int
}