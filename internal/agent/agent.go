package agent

import (
	"context"
	"fmt"
)

// memory scope + vendor model tier 常量 (goconst 3+ 次复用)
const (
	memoryScopeProject = "project"

	// vendor 模型档位（Anthropic Claude 模型名：opus/sonnet/haiku）
	vendorModelOpus   = "opus"
	vendorModelSonnet = "sonnet"
	vendorModelHaiku  = "haiku"
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
//
//nolint:revive // stutter: agent.AgentSpec 是清晰命名（spec 字段易混淆），故意保留
type AgentSpec struct {
	// 来自 frontmatter
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	Model       string   `yaml:"model"` // "opus" / "sonnet" / "haiku"
	MaxTurns    int      `yaml:"maxTurns"`
	Memory      string   `yaml:"memory"` // "project" / "user" / "session"
	Skills      []string `yaml:"skills"`

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
	case vendorModelOpus, vendorModelSonnet, vendorModelHaiku:
		// OK
	default:
		return fmt.Errorf("agent spec %q: unknown model %q (must be opus/sonnet/haiku)", s.Name, s.Model)
	}
	if s.MaxTurns <= 0 {
		s.MaxTurns = 30 // 默认 30（与 vendor story-architect 一致）
	}
	if s.Memory == "" {
		s.Memory = memoryScopeProject // 默认 project scope
	}
	return nil
}

// Agent 运行时实例 (Sprint A1.1 + Sprint A5 完整化)
//
// 包含 AgentSpec + LLM router + tools + state。
// Sprint A5 实现 Agent.Run() 主循环。
type Agent struct {
	Spec *AgentSpec

	// Sprint A5 填充：
	Router          LLMChat         // LLM 路由（interface，便于测试 mock + 防 Go interface-nil 陷阱）
	Tools           *ToolAdapter    // agent/tools → llm.Tool 适配
	Mapping         *ModelMapping   // vendor model → Go provider
	State           *AgentState     // 运行期状态
	Translator      *PathTranslator // A5.9 vendor 路径翻译
	ProjectRoot     string          // 沙箱根
	DisallowedTools []string        // A5.13/14/16 vendor DisallowedTools enforce
}

// NewAgent 从 AgentSpec 构造 Agent 实例（基础版本）
func NewAgent(spec *AgentSpec) *Agent {
	return &Agent{Spec: spec}
}

// Run 入口 (Sprint A5.1 实现)
//
// 实际逻辑在 RunAgent() 里。Agent.Run 包装 cfg 注入，方便调用。
func (a *Agent) Run(ctx context.Context, userInput string) (*Result, error) {
	return RunAgent(ctx, a.Spec, AgentLoopConfig{
		Router:          a.Router,
		Tools:           a.Tools,
		Mapping:         a.Mapping,
		ProjectRoot:     a.ProjectRoot,
		Translator:      a.Translator,
		DisallowedTools: a.DisallowedTools,
	}, userInput)
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
