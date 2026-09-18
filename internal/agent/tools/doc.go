// Package tools 实现 vendor oh-story-dsh-0.1.9 的 6 个基础 Tools (Sprint A2)
//
// vendor Claude Code 风格 tool 设计：
//   - Name():       工具名 (Read/Glob/Grep/Write/Edit/Bash)
//   - Description(): LLM 看的描述
//   - InputSchema(): JSON Schema (input 参数)
//   - Execute():     执行并返回结果
//
// Sprint A2 设计目标：
//   - 沙箱安全 (所有文件工具受 Sandbox 限制)
//   - Bash 严格白名单 (拒绝 rm/sudo/curl 等危险命令)
//   - Tool 通过 ToolRegistry 单点调用
//   - 工具可在 Agent.Run() 中被 LLM 通过 tool_use 调用
//
// 暂不实现 (Sprint A5 才做)：
//   - Agent.Run() 主循环 + maxTurns
//   - LLM tool_use 协议解析
package tools

// Tool 工具抽象 (vendor Claude Code 风格)
//
// 实现此接口即可注册到 ToolRegistry，供 Agent framework 调用。
type Tool interface {
	// Name 工具名 (Read/Glob/Grep/Write/Edit/Bash)
	Name() string

	// Description 工具描述（给 LLM 看，说明工具用途）
	Description() string

	// InputSchema JSON Schema 定义 input 参数
	InputSchema() []byte

	// Execute 执行工具，input 是 LLM 填的 JSON 参数（按 InputSchema 解）
	Execute(ctx *ExecContext, input []byte) (Result, error)
}

// ExecContext 工具执行上下文（沙箱根目录 + 可选 metadata）
type ExecContext struct {
	// Root 沙箱根目录。所有文件操作必须在此目录下。
	Root string

	// WorkingDir 工具执行的当前工作目录（默认 = Root）
	WorkingDir string

	// 工具间共享的状态（目前预留，A5 才用）
	State map[string]any
}

// Result 工具执行结果
type Result struct {
	// Content 给 LLM 看的文本结果
	Content string

	// IsError true 表示工具调用失败（不是 catastrophic error）
	IsError bool
}

// SuccessResult 构造成功结果
func SuccessResult(content string) Result {
	return Result{Content: content}
}

// ErrorResult 构造错误结果（工具正常返回但报告失败）
func ErrorResult(content string) Result {
	return Result{Content: content, IsError: true}
}
