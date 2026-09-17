package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool LLM 可调用的工具定义 (Sprint 34).
//
// 设计原则:
//   - Provider 无关: OpenAI / Anthropic / DashScope / DeepSeek 都用同一 Tool 定义
//   - JSON Schema 参数: Parameters 必须是 valid JSON Schema object
//   - 同步 Handler: ctx 可取消, 返回 (result, error), result 会被 JSON marshal
//
// 用法 (Sprint 34.5): memory.MemoryGraphTools 注册 4 个 tool
//
//	(graph_query / foreshadow_query / timeline_query / character_query)
//	注入到 Router.ChatWithTools, LLM 可主动调.
type Tool struct {
	// Name tool 名 (用于 OpenAI/Anthropic tool call ID). 必须唯一.
	Name string

	// Description 描述 (给 LLM 看的, 决定是否调用).
	Description string

	// Parameters JSON Schema object 定义参数.
	// 例: {"type":"object", "properties":{"name":{"type":"string"}}, "required":["name"]}
	Parameters json.RawMessage

	// Handler 实际执行函数.
	Handler ToolHandler
}

// ToolHandler tool 执行函数.
//
// 参数:
//   - ctx: 上下文 (LLM 取消 / 超时会传播)
//   - args: LLM 调 tool 时传的 JSON 参数 (与 Parameters schema 对应)
//
// 返回:
//   - result: 任意可 JSON marshal 的值 (会回灌给 LLM)
//   - error: 调用失败 (会作为 tool_result error 字段)
type ToolHandler func(ctx context.Context, args json.RawMessage) (result any, err error)

// ToolCall LLM 返回的 tool call 决策.
type ToolCall struct {
	// ID 唯一 ID (OpenAI: call_xxx, Anthropic: toolu_xxx).
	ID string

	// Name 要调的 tool 名.
	Name string

	// Arguments tool 参数 (JSON object).
	Arguments json.RawMessage
}

// ToolResult 一次 tool 调用的结果.
type ToolResult struct {
	// CallID 对应的 ToolCall.ID.
	CallID string

	// Result Handler 返回值 (会 JSON marshal 回灌给 LLM).
	Result any

	// Error Handler 返回的错误 (非空时 result 忽略, LLM 看到 error 字符串).
	Error string
}

// ToToolResultJSON 把 ToolResult 序列化成 LLM 可读格式 (map[string]any).
//
// 格式:
//
//	{"call_id": "xxx", "result": <handler 返回值>}
//	或 {"call_id": "xxx", "error": "<错误信息>"}
func (r *ToolResult) ToToolResultJSON() map[string]any {
	out := map[string]any{"call_id": r.CallID}
	if r.Error != "" {
		out["error"] = r.Error
	} else {
		out["result"] = r.Result
	}
	return out
}

// ChatWithToolsRequest 带 tool 的 chat 请求 (非流式).
//
// Sprint 34 核心: LLM 调 tool 后回灌, 重复直到 LLM 返回 final answer.
// 不做流式 (tool call 通常不需要流, 业务上是"调工具拿到数据, 再生成回答"模式).
type ChatWithToolsRequest struct {
	Messages []Message

	// Tools LLM 可调用的工具列表.
	Tools []Tool

	// ToolChoice 工具选择策略.
	//   "auto"  - LLM 决定调不调 (默认)
	//   "any"   - 强制调一个
	//   "none"  - 禁止调
	//   "<tool_name>" - 强制调指定 tool
	ToolChoice string

	// Task 路由任务类型 (复用 Router 路由表).
	Task TaskType

	// Override 覆盖默认 model/provider (空 = 走路由).
	OverrideProvider ProviderName
	OverrideModel    string

	// 控制参数.
	MaxTokens   int
	Temperature float64

	// MaxToolRounds 限制 multi-turn 轮数 (默认 5, 防 LLM 死循环).
	MaxToolRounds int
}

// ChatWithToolsResponse ChatWithTools 调用结果.
type ChatWithToolsResponse struct {
	// Content LLM 的最终回答 (可能为空如果 LLM 只调了 tool).
	Content string

	// ToolCalls LLM 在此次会话中实际调用的 tool 列表 (按调用顺序).
	ToolCalls []ExecutedToolCall

	// Provider/Model 实际使用的 (含 Override 解析结果).
	Provider ProviderName
	Model    string

	// TokensIn/Out 总 token (包含多轮).
	TokensIn  int
	TokensOut int
}

// ExecutedToolCall 一次实际执行的 tool call 记录.
type ExecutedToolCall struct {
	Call   ToolCall
	Result ToolResult
}

// ErrToolNotFound 调了未注册的 tool.
type ErrToolNotFound struct{ Name string }

func (e *ErrToolNotFound) Error() string {
	return fmt.Sprintf("tool %q not found", e.Name)
}
