package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/agent/tools"
	"github.com/ourvps1688/novel2all-go/internal/llm"
)

var _ LLMChat = (*llm.Router)(nil) // compile-time check

// AgentLoopConfig Agent.Run() 依赖的配置 (Sprint A5.1)
//
//nolint:revive // stutter: agent.LoopConfig 字段都是 agent 上下文，但 LoopConfig 是常用名
type LoopConfig struct {
	// Router LLM 路由器（接口便于测试 mock）
	Router LLMChat

	// Tools tool 适配器（agent/tools.Registry → llm.Tool）
	Tools *ToolAdapter

	// Mapping vendor model (opus/sonnet/haiku) → Go provider+model
	Mapping *ModelMapping

	// ProjectRoot 沙箱根目录
	ProjectRoot string

	// Translator prompt 路径翻译器（A5.9 处理 vendor .claude/skills 路径）
	Translator *PathTranslator

	// DisableTools vendor DisallowedTools enforce（A5.13, A5.14）
	DisallowedTools []string
}

// LLMChat ChatWithTools 接口（用于解耦 Router + mock 测试）
type LLMChat interface {
	ChatWithTools(ctx context.Context, req llm.ChatWithToolsRequest) (*llm.ChatWithToolsResponse, error)
}

// Run Agent 主循环 (Sprint A5.1 + A5.2 + A5.5 + A5.14 + A5.16)
//
// 流程：
//  1. 构造 messages [system_prompt, user_input]
//  2. for turn < maxTurns:
//     a. LLM.ChatWithTools(messages, tool_schemas)
//     b. 若无 tool_calls → 返回 final Content
//     c. 逐个执行 tool_calls，加 tool_result message
//  3. 超过 maxTurns → error
//
// 返回 Result.Content（LLM final answer）+ Tokens 统计。
func RunAgent(ctx context.Context, spec *AgentSpec, cfg LoopConfig, userInput string) (*Result, error) {
	if cfg.Router == nil {
		return nil, fmt.Errorf("agent.Run: Router required")
	}
	if cfg.Tools == nil {
		return nil, fmt.Errorf("agent.Run: Tools adapter required")
	}
	if cfg.Mapping == nil {
		cfg.Mapping = DefaultModelMapping()
	}
	if cfg.Translator == nil {
		cfg.Translator = NewPathTranslator()
	}
	if cfg.ProjectRoot == "" {
		cfg.ProjectRoot = "."
	}
	cfg.Tools.SetSandboxRoot(cfg.ProjectRoot)
	cfg.Tools.SetDisallowedTools(cfg.DisallowedTools) // A5.16 防御层

	// 1. 准备 system prompt（路径翻译 + Claude→中文 LLM 适配）
	systemPrompt := spec.SystemPrompt
	systemPrompt = cfg.Translator.Translate(systemPrompt)
	if NeedsAdaptation(systemPrompt) {
		systemPrompt = AdaptPrompt(systemPrompt)
	}

	// 2. 准备 tools（vendor DisallowedTools enforce，A5.14 + A5.16）
	llmTools := buildLLMTools(spec.Tools, cfg.Tools, cfg.DisallowedTools)
	if len(llmTools) == 0 {
		llmTools = nil // 无 tool 时传 nil（LLM 不知道有 tool）
	}

	// 3. 决定 LLM provider + model（vendor model → 实际 model）
	_, realModel, err := cfg.Mapping.Map(spec.Model)
	if err != nil {
		return nil, fmt.Errorf("agent.Run: %w", err)
	}

	// 4. messages 起始
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userInput},
	}

	totalIn, totalOut := 0, 0
	var allToolCalls []ExecutedToolCall // 累积所有 turn 的 tool calls

	// 5. 主循环
	for turn := 0; turn < spec.MaxTurns; turn++ {
		// 检查 ctx 取消
		if ctx.Err() != nil {
			return nil, fmt.Errorf("agent.Run: ctx canceled at turn %d: %w", turn, ctx.Err())
		}

		resp, err := cfg.Router.ChatWithTools(ctx, llm.ChatWithToolsRequest{
			Messages:         messages,
			Tools:            llmTools,
			ToolChoice:       "auto",
			OverrideProvider: "", // 走 Router 路由表
			OverrideModel:    realModel,
			MaxTokens:        4096,
			MaxToolRounds:    1, // 每次 ChatWithTools 只 1 轮（Agent 层做 multi-turn）
		})
		if err != nil {
			return nil, fmt.Errorf("agent.Run: turn %d ChatWithTools: %w", turn, err)
		}
		totalIn += resp.TokensIn
		totalOut += resp.TokensOut

		// 收集所有 tool calls
		allToolCalls = append(allToolCalls, convertExecutedToolCalls(resp.ToolCalls)...)

		// 无 tool_calls → final answer
		if len(resp.ToolCalls) == 0 {
			return &Result{
				Content:   resp.Content,
				TokensIn:  totalIn,
				TokensOut: totalOut,
				ToolCalls: allToolCalls,
			}, nil
		}

		// 6. 处理每个 tool_call
		for _, ec := range resp.ToolCalls {
			toolResult := cfg.Tools.Dispatch(ec.Call.Name, ec.Call.Arguments)

			// 构造 tool_result message
			toolMsg := buildToolResultMessage(ec.Call, toolResult)
			messages = append(messages, toolMsg)
		}
	}

	return nil, fmt.Errorf("agent.Run: max turns (%d) exceeded", spec.MaxTurns)
}

// buildLLMTools 构造 LLM tool schemas（A5.14 DisallowedTools enforce）
func buildLLMTools(specTools []string, adapter *ToolAdapter, disallowed []string) []llm.Tool {
	allowed := make([]string, 0, len(specTools))
	disallowedSet := make(map[string]bool, len(disallowed))
	for _, d := range disallowed {
		disallowedSet[d] = true
	}
	for _, name := range specTools {
		if disallowedSet[name] {
			continue // A5.14: enforce DisallowedTools
		}
		allowed = append(allowed, name)
	}
	return adapter.LLMTools(allowed)
}

// buildToolResultMessage 构造 tool result message 给 LLM 下一轮读
//
//	anthropic 格式：role="user", content 是 tool_result block
//	openai 格式：role="tool", content 是 JSON
//
// 我们的 router.ChatWithTools 内部会按 provider 转换，这里给通用 message：
//
//	role="user" + content 是 JSON 字符串（既适配 anthropic 也适配 openai 的 toToolResultJSON）
func buildToolResultMessage(call llm.ToolCall, result tools.Result) llm.Message {
	contentJSON, _ := json.Marshal(map[string]any{
		"call_id":  call.ID,
		"content":  result.Content,
		"is_error": result.IsError,
	})
	return llm.Message{
		Role:    "user",
		Content: string(contentJSON),
	}
}

// convertExecutedToolCalls llm.ExecutedToolCall → agent.ExecutedToolCall
func convertExecutedToolCalls(src []llm.ExecutedToolCall) []ExecutedToolCall {
	out := make([]ExecutedToolCall, 0, len(src))
	for _, ec := range src {
		out = append(out, ExecutedToolCall{
			Name:   ec.Call.Name,
			Input:  string(ec.Call.Arguments),
			Output: stringResult(ec.Result.Result),
			Error:  ec.Result.Error,
		})
	}
	return out
}

func stringResult(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return strings.Trim(string(b), "\"")
}
