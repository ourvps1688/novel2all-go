package agent

import (
	"context"
	"fmt"
)

// AgentSpec 解析 vendor role .md frontmatter + body 后的规格 (Sprint A1.1)
//
// 对应 vendor oh-story-dsh-0.1.9 的 agent spec:
//   - name:        vendor role name (e.g. "story-architect")
//   - description: 角色职责描述 (multiline)
//   - tools:       允许使用的工具列表 (e.g. ["Read", "Glob", "Grep"])
//   - model:       vendor 档位 (opus / sonnet / haiku)
//   - maxTurns:    Agent.Run() 最大循环轮数
//   - memory:      memory scope (project / user / session)
//   - skills:      引用的 skill 名列表（用于按需加载 reference）
//   - systemPrompt: role .md 的 body 部分（去掉 frontmatter 后）
type AgentSpec struct {
	// 来自 frontmatter
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Tools        []string `yaml:"tools"`
	Model        string   `yaml:"model"` // "opus" / "sonnet" / "haiku"
	MaxTurns     int      `yaml:"maxTurns"`
	Memory       string   `yaml:"memory"` // "project" / "user" / "session"
	Skills       []string `yaml:"skills"`

	// body 部分（去掉 --- frontmatter --- 之后）
	SystemPrompt string

	// 元数据
	SourcePath string // role .md 文件路径（用于调试/错误信息）
}

// Validate 校验 AgentSpec 是否合法
func (s *AgentSpec) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("agent spec: name is required")
	}
	if s.Model == "" {
		return fmt.Errorf("agent spec %q: model is required", s.Name)
	}
	switch s.Model {
	case "opus", "sonnet", "haiku":
		// OK
	default:
		return fmt.Errorf("agent spec %q: unknown model %q (must be opus/sonnet/haiku)", s.Name, s.Model)
	}
	if s.MaxTurns <= 0 {
		s.MaxTurns = 30 // 默认 30（与 vendor story-architect 一致）
	}
	if s.Memory == "" {
		s.Memory = "project" // 默认 project scope
	}
	return nil
}

// Agent 运行时实例 (Sprint A1.1)
//
// 包含 AgentSpec + LLM router + memory + tools + state。
// Sprint A5 才实现 Agent.Run() 主循环；A1 阶段只构建数据结构。
type Agent struct {
	Spec *AgentSpec

	// Sprint A5 才填充：
	//   ProjectRoot string
	//   Tools    map[string]Tool
	//   LLM      *llm.Router
	//   Memory   *memory.MemoryManager
	//   State    *AgentState
}

// NewAgent 从 AgentSpec 构造 Agent 实例（基础版本，A1 不含 Run 循环）
func NewAgent(spec *AgentSpec) *Agent {
	return &Agent{Spec: spec}
}

// Run 入口（占位 — Sprint A5 实现完整 maxTurns + tool call 循环）
//
// A1 阶段：返回 "not implemented" 错误，确保测试可断言。
// A5 阶段：实现完整循环（每轮调 LLM → 解析 tool_use → 调 Tool.Handler → 把 result 塞回 messages）。
func (a *Agent) Run(ctx context.Context, userInput string) (*Result, error) {
	return nil, fmt.Errorf("agent.Run: not implemented yet (planned for Sprint A5, see docs/p3-vendor-alignment-plan.md A5.1)")
}

// Result Agent.Run 返回值 (Sprint A5 才完整实现, A1 阶段先定义结构)
type Result struct {
	Content   string
	ToolCalls []ExecutedToolCall
	TokensIn  int
	TokensOut int
}

// ExecutedToolCall 一次 tool call 的执行结果（A1 占位）
type ExecutedToolCall struct {
	Name   string
	Input  string
	Output string
	Error  string
}
