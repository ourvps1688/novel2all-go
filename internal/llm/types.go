// Package llm 提供 LLM 多 provider 路由 + 流式响应。
//
// P1 阶段支持 4 家 provider：
//   - dashscope (阿里云通义千问, OpenAI 兼容)
//   - deepseek  (OpenAI 兼容)
//   - minimax   (⚠️ 必须用 Anthropic 兼容协议,不是 OpenAI)
//   - anthropic (Anthropic 官方)
//
// 路由策略：按 TaskType 自动选 provider + model
// （延续 Python V1.5.5 决策：WRITING → minimax，其他 → deepseek）
package llm

import (
	"context"
	"errors"
)

// TaskType 路由任务的分类
type TaskType string

const (
	TaskWriting       TaskType = "WRITING"       // 长文/正文 → minimax
	TaskConsistency   TaskType = "CONSISTENCY"   // 一致性检查 → deepseek
	TaskExtraction    TaskType = "EXTRACTION"    // 角色/伏笔提取 → deepseek
	TaskSummarization TaskType = "SUMMARIZATION" // 摘要 → deepseek
	TaskCover         TaskType = "COVER"         // 封面文案 → deepseek
	TaskUnknown       TaskType = "UNKNOWN"       // 兜底 → deepseek
)

// ProviderName provider 名称（字符串枚举）
type ProviderName string

const (
	ProviderDashScope ProviderName = "dashscope"
	ProviderDeepSeek  ProviderName = "deepseek"
	ProviderMinimax   ProviderName = "minimax"
	ProviderAnthropic ProviderName = "anthropic"
)

// Message 消息
type Message struct {
	Role    string // "system" | "user" | "assistant"
	Content string
}

// Request 完整请求
type Request struct {
	Task     TaskType
	Messages []Message
	// 可选覆盖：provider/model
	OverrideProvider ProviderName // 空 = 用路由
	OverrideModel    string       // 空 = 用路由默认 model
	MaxTokens        int          // 0 = 用 provider 默认
	Temperature      float64      // 0 = 用 provider 默认
	// 流式（默认 true）
	Stream bool
}

// Chunk 流式响应的一个分片
type Chunk struct {
	Content string // 增量文本
	Done    bool   // 是否最后一个分片
	Err     error  // 错误（如果有）
}

// Response 非流式完整响应
type Response struct {
	Content   string
	Provider  ProviderName
	Model     string
	TokensIn  int
	TokensOut int
}

// Provider LLM provider 抽象
type Provider interface {
	// Name 返回 provider 名
	Name() ProviderName

	// DefaultModel 返回 provider 的默认模型名（用于 /api/models 列出 + /api/model/switch 校验）
	DefaultModel() string

	// APIBase 返回 provider 的 base URL（用于 /api/models 调试展示；空字符串 = 用 SDK 默认）
	APIBase() string

	// ChatStream 流式调用，发送分片到 ch，结束时关闭 ch
	ChatStream(ctx context.Context, req Request, ch chan<- Chunk) error

	// Chat 非流式调用
	Chat(ctx context.Context, req Request) (*Response, error)

	// Available 检查 API key 是否配置
	Available() bool
}

// Common errors
var (
	ErrNoProvider         = errors.New("no provider available for request")
	ErrAllProvidersFailed = errors.New("all providers failed")
)
