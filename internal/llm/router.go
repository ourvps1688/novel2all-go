package llm

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Router 根据 task type 选 provider + model，并支持 fallback。
type Router struct {
	mu        sync.RWMutex
	providers map[ProviderName]Provider

	// 路由表：TaskType → (Provider, Model)
	routes map[TaskType]routeConfig
}

type routeConfig struct {
	Provider ProviderName
	Model    string
}

// 默认模型（延续 Python V1.5.5 决策）
const (
	DefaultDashScopeModel = "qwen-plus"
	DefaultDeepSeekModel  = "deepseek-chat"
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
	p, model, err := r.resolve(req)
	if err != nil {
		return err
	}
	req2 := req
	if req2.OverrideModel == "" {
		req2.OverrideModel = model
	}
	return p.ChatStream(ctx, req2, ch)
}

// Chat 非流式调用（自动路由）
func (r *Router) Chat(ctx context.Context, req Request) (*Response, error) {
	p, model, err := r.resolve(req)
	if err != nil {
		return nil, err
	}
	req2 := req
	if req2.OverrideModel == "" {
		req2.OverrideModel = model
	}
	resp, err := p.Chat(ctx, req2)
	if err != nil {
		return nil, err
	}
	if resp.Provider == "" {
		resp.Provider = p.Name()
	}
	if resp.Model == "" {
		resp.Model = model
	}
	return resp, nil
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
