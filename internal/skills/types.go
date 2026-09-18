package skills

// Package skills 提供 novel2all-go 的 skill 加载与执行能力。
//
// Sprint A4 改造：13 个 SKILL.md + 242 references 通过 embed.FS 嵌入二进制。
// 目录结构：assets/<skill>/SKILL.md + assets/<skill>/references/[<subdir>/]*.md
//
// 按需加载（决策 3=B）：
//   - NewLoader() 加载所有 SKILL.md frontmatter + body + 列出 references 元数据
//   - LoadReference(skill, refPath) 按需读 reference content（带 L1 cache）

// ExecuteInput skill 执行输入（保持向后兼容，Sprint 32+ 引入）
type ExecuteInput struct {
	// SkillName skill 名（不含 .md）
	SkillName string

	// UserInput 用户输入（作为 user message）
	UserInput string

	// SystemInput 系统消息（V0.30+ Sprint 32）.
	// 拼接顺序: SystemInput → skill.Body → UserInput.
	// 空 = 走原路径（只 skill.Body + UserInput）保持 V0.29 兼容.
	SystemInput string

	// Variables 模板变量（用于未来扩展，V0.1 暂未使用）
	Variables map[string]any

	// 可选：路由 task 类型（WRITING/CONSISTENCY 等），空 = 走 router 默认（deepseek）
	Task string

	// 可选：强制 provider（dashscope/deepseek/minimax/anthropic），空 = 走路由
	Provider string

	// 可选：强制 model 名（覆盖 provider 默认 model）
	Model string
}

// ExecuteResult skill 执行结果（非流式）
type ExecuteResult struct {
	Content   string
	Provider  string
	Model     string
	TokensIn  int
	TokensOut int
}
