// Package llm 提供 LLM 多 provider 路由 + 流式响应。
//
// P1 阶段支持 4 家 provider：
//   - dashscope (阿里云通义千问, Anthropic 兼容, base_url=/apps/anthropic)
//   - deepseek  (Anthropic 兼容, base_url=/anthropic, claude-opus-* 自动 → deepseek-v4-pro)
//   - minimax   (⚠️ 必须用 Anthropic 兼容协议, MiniMax-M3 国内版, 1M context)
//   - anthropic (Anthropic 官方)
//
// 路由策略：按 TaskType 自动选 provider + model
// （延续 Python V1.5.5 决策：WRITING → minimax，其他 → deepseek）
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MetricsHook LLM 调用的可观测性钩子（P1-F 切片 9）。
//
// 用于解耦 llm 包和 obs 包：llm 包定义接口，main.go 注入 obs.Metrics + obs.TraceRecorder 的 adapter 实现。
// 不传钩子时 Router 行为不变（向后兼容）。
type MetricsHook interface {
	IncLLMCall(provider, task, status string)
	AddLLMTokens(provider, kind string, n int64)
	RecordLLMTrace(provider, task, status string, duration time.Duration, tokensIn, tokensOut int)
}

// Router 根据 task type 选 provider + model，并支持 fallback。
type Router struct {
	mu        sync.RWMutex
	providers map[ProviderName]Provider

	// 路由表：TaskType → (Provider, Model)
	routes map[TaskType]routeConfig

	// metrics 钩子（P1-F 切片 9，可选）
	hook atomic.Pointer[MetricsHook]

	// cache 钩子（Sprint 15 commit G，可选）
	cache atomic.Pointer[Cache]

	// adaptive 路由（Sprint 21，可选；不注入时用 routes 表默认）
	adaptive atomic.Pointer[AdaptiveRouter]
}

type routeConfig struct {
	Provider ProviderName
	Model    string
}

// 默认模型（Sprint A1.16/A1.21, 2026-09-18 更新）
//
// 选型逻辑（实测官方文档）：
//   - DefaultDashScopeModel = "qwen3.7-plus"  千问真实 model 名（千问无 claude-* 自动映射）
//   - DefaultDeepSeekModel  = "claude-opus-4-5-20250929"  DeepSeek 自动映射到 deepseek-v4-pro
//   - DefaultMinimaxModel   = "MiniMax-M3"   国内版 1M context
//   - DefaultAnthropicModel = "claude-3-5-sonnet-20241022"  Anthropic 官方
//
// 三个中文 LLM 全部 1M context（2026-09-18 实测）：
//   - MiniMax-M3:    https://platform.minimax.cn/docs/api-reference/text-anthropic-api
//   - DeepSeek V4:   https://api-docs.deepseek.com/quick_start/pricing
//   - 千问 Plus:     https://help.aliyun.com/zh/model-studio/text-generation-model
const (
	DefaultDashScopeModel = "qwen3.7-plus"
	DefaultDeepSeekModel  = "claude-opus-4-5-20250929"
	DefaultMinimaxModel   = "MiniMax-M3"
	DefaultAnthropicModel = "claude-3-5-sonnet-20241022"
)

// NewRouter 创建 Router，自动注册所有 provider
func NewRouter(cfg Config) *Router {
	r := &Router{
		providers: make(map[ProviderName]Provider),
		routes:    defaultRoutes(),
	}

	if cfg.DashScopeAPIKey != "" {
		r.providers[ProviderDashScope] = NewDashScope(cfg.DashScopeAPIKey)
	}
	if cfg.DeepSeekAPIKey != "" {
		r.providers[ProviderDeepSeek] = NewDeepSeek(cfg.DeepSeekAPIKey)
	}
	if cfg.MinimaxAPIKey != "" {
		r.providers[ProviderMinimax] = NewMinimax(cfg.MinimaxAPIKey)
	}
	if cfg.AnthropicAPIKey != "" {
		r.providers[ProviderAnthropic] = NewAnthropic(cfg.AnthropicAPIKey)
	}

	return r
}

func defaultRoutes() map[TaskType]routeConfig {
	return map[TaskType]routeConfig{
		TaskWriting:       {Provider: ProviderMinimax, Model: DefaultMinimaxModel},
		TaskConsistency:   {Provider: ProviderDeepSeek, Model: DefaultDeepSeekModel},
		TaskExtraction:    {Provider: ProviderDeepSeek, Model: DefaultDeepSeekModel},
		TaskSummarization: {Provider: ProviderDeepSeek, Model: DefaultDeepSeekModel},
		TaskCover:         {Provider: ProviderDeepSeek, Model: DefaultDeepSeekModel},
		TaskUnknown:       {Provider: ProviderDeepSeek, Model: DefaultDeepSeekModel},
	}
}

// AvailableProviders 列出已配置 API key 的 provider
func (r *Router) AvailableProviders() []ProviderName {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderName, 0, len(r.providers))
	for p := range r.providers {
		out = append(out, p)
	}
	return out
}

// ProviderByName 按名查 provider（返回 nil 如果未配置）
func (r *Router) ProviderByName(name ProviderName) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[name]
}

// resolve 解析请求到具体 provider + model
func (r *Router) resolve(req Request) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 显式 override 优先
	if req.OverrideProvider != "" {
		p, ok := r.providers[req.OverrideProvider]
		if !ok {
			return nil, "", fmt.Errorf("override provider %q not configured", req.OverrideProvider)
		}
		model := req.OverrideModel
		if model == "" {
			model = defaultModelFor(req.OverrideProvider)
		}
		return p, model, nil
	}

	// 路由表
	cfg, ok := r.routes[req.Task]
	if !ok {
		cfg = r.routes[TaskUnknown]
	}
	p, ok := r.providers[cfg.Provider]
	if !ok {
		// fallback to deepseek
		if p2, ok2 := r.providers[ProviderDeepSeek]; ok2 {
			return p2, DefaultDeepSeekModel, nil
		}
		return nil, "", fmt.Errorf("task %q routes to %q but not configured", req.Task, cfg.Provider)
	}
	return p, cfg.Model, nil
}

func defaultModelFor(p ProviderName) string {
	switch p {
	case ProviderDashScope:
		return DefaultDashScopeModel
	case ProviderDeepSeek:
		return DefaultDeepSeekModel
	case ProviderMinimax:
		return DefaultMinimaxModel
	case ProviderAnthropic:
		return DefaultAnthropicModel
	}
	return ""
}

// ChatStream 流式调用（自动路由）
func (r *Router) ChatStream(ctx context.Context, req Request, ch chan<- Chunk) error {
	start := time.Now()
	p, model, err := r.resolve(req)
	if err != nil {
		r.recordCall("", string(req.Task), "resolve_error", 0, 0)
		return err
	}
	req2 := req
	if req2.OverrideModel == "" {
		req2.OverrideModel = model
	}
	if err := p.ChatStream(ctx, req2, ch); err != nil {
		r.recordCall(string(p.Name()), string(req.Task), "error", 0, 0)
		r.recordTrace(string(p.Name()), string(req.Task), "error", time.Since(start), 0, 0)
		return err
	}
	r.recordCall(string(p.Name()), string(req.Task), "success", 0, 0)
	r.recordTrace(string(p.Name()), string(req.Task), "success", time.Since(start), 0, 0)
	return nil
}

// Chat 非流式调用（自动路由 + cache 集成）
func (r *Router) Chat(ctx context.Context, req Request) (*Response, error) {
	// Cache lookup（仅非流式；ChatStream 不缓存 chunks）
	if cp := r.cache.Load(); cp != nil {
		prompt := r.promptFromRequest(req)
		cached, hit := (*cp).Get(ctx, req.Task, "", prompt)
		if hit {
			r.recordCall("", string(req.Task), "cache_hit", cached.TokensIn, cached.TokensOut)
			return cached, nil
		}
	}

	start := time.Now()
	p, model, err := r.resolve(req)
	if err != nil {
		r.recordCall("", string(req.Task), "resolve_error", 0, 0)
		return nil, err
	}
	req2 := req
	if req2.OverrideModel == "" {
		req2.OverrideModel = model
	}
	resp, err := p.Chat(ctx, req2)
	if err != nil {
		r.recordCall(string(p.Name()), string(req.Task), "error", 0, 0)
		r.recordTrace(string(p.Name()), string(req.Task), "error", time.Since(start), 0, 0)
		return nil, err
	}
	if resp.Provider == "" {
		resp.Provider = p.Name()
	}
	if resp.Model == "" {
		resp.Model = model
	}
	r.recordCall(string(p.Name()), string(req.Task), "success", resp.TokensIn, resp.TokensOut)
	r.recordTrace(string(p.Name()), string(req.Task), "success", time.Since(start), resp.TokensIn, resp.TokensOut)
	// Cache write（异步，不影响返回）
	if cp := r.cache.Load(); cp != nil {
		prompt := r.promptFromRequest(req)
		_ = (*cp).Set(ctx, req.Task, model, prompt, resp)
	}
	return resp, nil
}

// SetMetricsHook 注入 metrics 钩子（可选；不注入时 metrics 调用为 no-op）。
func (r *Router) SetMetricsHook(h MetricsHook) {
	r.hook.Store(&h)
}

// ChatWithTools multi-turn tool call 调用 (Sprint 34).
//
// 流程 (每轮):
//  1. Router 调 Provider.ChatWithTools (一次 LLM 调用)
//  2. 如果 LLM 返回 ToolCalls → 调对应 Tool.Handler, 收集 ToolResults
//  3. 把 tool_results 塞回 messages, 重复
//  4. 直到 LLM 返回空 ToolCalls (final answer) 或达到 MaxToolRounds
//
// Sprint 34 简化: 文本格式 tool result (assistant 决策 + tool result 拼文本进 messages).
// 后续 Sprint 35+ 再做结构化 tool result format (按 provider 协议).
func (r *Router) ChatWithTools(ctx context.Context, req ChatWithToolsRequest) (*ChatWithToolsResponse, error) {
	if len(req.Tools) == 0 {
		return nil, fmt.Errorf("ChatWithTools: no tools provided")
	}

	// tool name → handler 索引
	toolIndex := make(map[string]Tool, len(req.Tools))
	for _, t := range req.Tools {
		toolIndex[t.Name] = t
	}

	maxRounds := req.MaxToolRounds
	if maxRounds == 0 {
		maxRounds = 5
	}

	messages := make([]Message, len(req.Messages))
	copy(messages, req.Messages)

	allCalls := make([]ExecutedToolCall, 0)
	totalTokensIn := 0
	totalTokensOut := 0
	// resolve() 需要 Request 类型, 传 Task + OverrideProvider 即可
	provider, model, err := r.resolve(Request{
		Task:             req.Task,
		OverrideProvider: req.OverrideProvider,
		OverrideModel:    req.OverrideModel,
	})
	if err != nil {
		return nil, err
	}
	var providerName ProviderName
	if req.OverrideProvider != "" {
		providerName = req.OverrideProvider
	} else {
		providerName = provider.Name()
	}

	for round := 0; round < maxRounds; round++ {
		// 1. 调一次 LLM
		stepReq := req
		stepReq.Messages = messages
		stepReq.OverrideProvider = providerName
		stepReq.OverrideModel = model

		resp, err := provider.ChatWithTools(ctx, stepReq)
		if err != nil {
			return nil, fmt.Errorf("round %d: %w", round, err)
		}
		totalTokensIn += resp.TokensIn
		totalTokensOut += resp.TokensOut

		// 2. 如果 LLM 不调 tool, 终止 (返回 final answer)
		if len(resp.ToolCalls) == 0 {
			return &ChatWithToolsResponse{
				Content:   resp.Content,
				ToolCalls: allCalls,
				Provider:  providerName,
				Model:     resp.Model,
				TokensIn:  totalTokensIn,
				TokensOut: totalTokensOut,
			}, nil
		}

		// 3. 处理每个 tool call
		var toolResults []map[string]any
		for _, ec := range resp.ToolCalls {
			tool, ok := toolIndex[ec.Call.Name]
			var result ToolResult
			result.CallID = ec.Call.ID
			if !ok {
				result.Error = (&ErrToolNotFound{Name: ec.Call.Name}).Error()
			} else {
				val, err := tool.Handler(ctx, ec.Call.Arguments)
				if err != nil {
					result.Error = err.Error()
				} else {
					result.Result = val
				}
			}
			ec.Result = result
			allCalls = append(allCalls, ec)
			toolResults = append(toolResults, result.ToToolResultJSON())
		}

		// 4. 拼 tool_results 进 messages (Sprint 34 简化: 文本格式)
		assistantMsg := "[assistant decided to call tools]\n"
		for _, ec := range resp.ToolCalls {
			argsJSON, _ := json.Marshal(ec.Call.Arguments)
			assistantMsg += fmt.Sprintf("- %s(%s): pending\n", ec.Call.Name, string(argsJSON))
		}
		messages = append(messages, Message{Role: "assistant", Content: assistantMsg})

		resultsMsg := "[tool results]\n"
		for _, tr := range toolResults {
			resJSON, _ := json.Marshal(tr)
			resultsMsg += string(resJSON) + "\n"
		}
		messages = append(messages, Message{Role: "user", Content: resultsMsg})
	}

	// 达到 maxRounds 还没收敛
	return &ChatWithToolsResponse{
		Content:   fmt.Sprintf("max tool rounds (%d) reached without final answer", maxRounds),
		ToolCalls: allCalls,
		Provider:  providerName,
		Model:     model,
		TokensIn:  totalTokensIn,
		TokensOut: totalTokensOut,
	}, nil
}

// SetCache 注入 LLM cache（Sprint 15 commit G，可选）
//
// 调用后 router.Chat 会先查 cache（hit 直接返回，不调 provider API）。
// nil 也允许（清除 cache），向后兼容。
func (r *Router) SetCache(c *Cache) {
	r.cache.Store(c)
}

// SetAdaptiveRouter 注入自适应路由（Sprint 21，可选）
//
// 调用后 router.resolve 会先调 AdaptiveRouter.Select(task) 选 model。
// 如果样本不足 (冷启动), 仍用 routes 表默认路由。
// nil 也允许（清除 adaptive routing），向后兼容。
func (r *Router) SetAdaptiveRouter(ar *AdaptiveRouter) {
	r.adaptive.Store(ar)
}

// promptFromRequest 从 Request.Messages 拼出完整 prompt 用于 cache key
//
// 简单拼接：user messages 的 Content + system prompt（如果有）
func (r *Router) promptFromRequest(req Request) string {
	var sb strings.Builder
	for _, m := range req.Messages {
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(m.Role)
		sb.WriteByte(':')
		sb.WriteString(m.Content)
	}
	return sb.String()
}

func (r *Router) recordCall(provider, task, status string, tokensIn, tokensOut int) {
	if p := r.hook.Load(); p != nil {
		(*p).IncLLMCall(provider, task, status)
		if tokensIn > 0 {
			(*p).AddLLMTokens(provider, "prompt", int64(tokensIn))
		}
		if tokensOut > 0 {
			(*p).AddLLMTokens(provider, "completion", int64(tokensOut))
		}
	}
}

func (r *Router) recordTrace(provider, task, status string, duration time.Duration, tokensIn, tokensOut int) {
	if p := r.hook.Load(); p != nil {
		(*p).RecordLLMTrace(provider, task, status, duration, tokensIn, tokensOut)
	}
}

// ErrProviderNotConfigured provider 未配置
var ErrProviderNotConfigured = errors.New("provider not configured")

// Config LLM provider 配置（从 internal/config.LLMConfig 镜像）
// 镜像内部 config.LLMConfig 是必要的，因为跨包共享结构会引入循环依赖
type Config struct {
	DashScopeAPIKey string
	DeepSeekAPIKey  string
	MinimaxAPIKey   string
	AnthropicAPIKey string
}
