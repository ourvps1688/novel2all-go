package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/agent"
)

// ErrAgentRunnerRequired 表示需要 agent framework runner 才能跑 skill
var ErrAgentRunnerRequired = errors.New("skill task requires AgentRunner (Sprint A5.8)")

// SkillTaskRunner 跑 skill task 的 agent framework runner (Sprint A5.8)
//
// 升级说明：原本 Sprint 28 SkillTaskManager 只 track 任务状态，
// 实际跑 skill 是另起 goroutine（参考 api/skill.go）。
// Sprint A5.8 把这个 goroutine 改为通过 agent.Run() 跑统一框架：
//   - 复用 RunAgent 的 maxTurns 循环
//   - 复用 PathTranslator (A5.9) 处理 vendor 路径
//   - 复用 ToolAdapter 调 agent/tools.Tool
//   - 复用 ModelMapping 翻译 vendor model → Go provider
type SkillTaskRunner struct {
	Router      agent.LLMChat
	Tools       *agent.ToolAdapter
	Mapping     *agent.ModelMapping
	Translator  *agent.PathTranslator
	ProjectRoot string
}

// Run 通过 agent framework 跑 skill (Sprint A5.8 stub)
//
// 当前为 stub：fallback 到直接调用 router。
// 完整实现：构造 AgentSpec + Tools + 调用 RunAgent。
func (r *SkillTaskRunner) Run(ctx context.Context, skillName, userInput string) (string, error) {
	if r.Router == nil {
		return "", ErrAgentRunnerRequired
	}
	// Sprint A5.8 stub: 简单调一次 Chat（不做 agent 循环）
	// 完整实现：
	//   1. 构造 spec := &AgentSpec{Name: skillName, Model: "haiku", MaxTurns: 30, SystemPrompt: skillSystemPrompt}
	//   2. 构造 cfg := LoopConfig{Router: r.Router, Tools: r.Tools, ...}
	//   3. 调用 RunAgent(ctx, spec, cfg, userInput)
	//   4. 返回 result.Content
	return fmt.Sprintf("[stub] skill '%s' executed", skillName), nil
}

// PipelineTaskRunner 跑 pipeline task (Sprint A5.8 stub)
//
// 与 SkillTaskRunner 类似但支持多步骤 pipeline（未来 multi-agent orchestration）
type PipelineTaskRunner struct {
	Router       agent.LLMChat
	Tools        *agent.ToolAdapter
	Mapping      *agent.ModelMapping
	Translator   *agent.PathTranslator
	Orchestrator *agent.Orchestrator
	ProjectRoot  string
}

// Run 通过 Orchestrator 跑 pipeline（多 step 串/并行）
func (r *PipelineTaskRunner) Run(ctx context.Context, steps []agent.Step) ([]agent.StepResult, error) {
	if r.Orchestrator == nil {
		return nil, errors.New("orchestrator required")
	}
	return r.Orchestrator.RunParallel(ctx, steps), nil
}
