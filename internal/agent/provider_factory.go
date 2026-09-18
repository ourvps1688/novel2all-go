package agent

import (
	"fmt"
	"os"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// ProviderFactory 统一 provider 工厂 (Sprint A1.18)
//
// 设计目标：
//   - 复用 internal/llm/ 现有 NewDeepSeek / NewDashScope / NewMinimax 构造函数
//   - 工厂层加 API key 校验（不返回空 apiKey 的 provider）
//   - 工厂层加统一 error handling
//   - 支持从 env vars 自动加载 (NewProviderFactoryFromEnv)
//   - 支持 ModelMapping 驱动的 CreateForVendorModel
//
// 为什么不在 internal/llm/ 加？因为：
//   - llm 包已经有 NewDeepSeek/NewDashScope/NewMinimax，无需重复
//   - 工厂是 agent framework 的编排层，不属于 llm 底层
//   - 未来 agent 可能加缓存 / 多账号 / 限流 等逻辑，独立 package 便于扩展
type ProviderFactory struct {
	// 复用 llm.Config（避免重复定义 APIKey 字段）
	cfg llm.Config
}

// NewProviderFactory 从显式 config 构造工厂
func NewProviderFactory(cfg llm.Config) *ProviderFactory {
	return &ProviderFactory{cfg: cfg}
}

// NewProviderFactoryFromEnv 从环境变量构造工厂
//
// env vars:
//   - DASHSCOPE_API_KEY
//   - DEEPSEEK_API_KEY
//   - MINIMAX_API_KEY
//   - ANTHROPIC_API_KEY (保留，但 agent framework 默认不用)
func NewProviderFactoryFromEnv() *ProviderFactory {
	return NewProviderFactory(llm.Config{
		DashScopeAPIKey: os.Getenv("DASHSCOPE_API_KEY"),
		DeepSeekAPIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		MinimaxAPIKey:   os.Getenv("MINIMAX_API_KEY"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
}

// CreateProvider 按 provider name 创建 anthropic-compat provider
//
// 2026-09-18 实测：3 provider 用不同 model 字段：
//   - deepseek → claude-opus-4-5-20250929 (DeepSeek 服务端自动 → deepseek-v4-pro)
//   - dashscope → qwen3.7-plus (千问真实名, 千问无自动映射)
//   - minimax → MiniMax-M3 (国内版, 自动注入 MiniMax defaults via NewMinimax)
//
// 若 API key 缺失，返回 error（不返回空 key 的 provider）。
func (f *ProviderFactory) CreateProvider(name llm.ProviderName) (llm.Provider, error) {
	switch name {
	case llm.ProviderDeepSeek:
		if f.cfg.DeepSeekAPIKey == "" {
			return nil, fmt.Errorf("provider %s: DEEPSEEK_API_KEY not configured", name)
		}
		// 复用 llm.NewDeepSeek：URL=https://api.deepseek.com/anthropic, model=DefaultDeepSeekModel=claude-opus-4-5-20250929
		return llm.NewDeepSeek(f.cfg.DeepSeekAPIKey), nil

	case llm.ProviderDashScope:
		if f.cfg.DashScopeAPIKey == "" {
			return nil, fmt.Errorf("provider %s: DASHSCOPE_API_KEY not configured", name)
		}
		// 复用 llm.NewDashScope：URL=https://dashscope.aliyuncs.com/apps/anthropic, model=qwen3.7-plus
		return llm.NewDashScope(f.cfg.DashScopeAPIKey), nil

	case llm.ProviderMinimax:
		if f.cfg.MinimaxAPIKey == "" {
			return nil, fmt.Errorf("provider %s: MINIMAX_API_KEY not configured", name)
		}
		// 复用 llm.NewMinimax：URL=国内版, model=MiniMax-M3, 已注入 MiniMax defaults
		return llm.NewMinimax(f.cfg.MinimaxAPIKey), nil

	case llm.ProviderAnthropic:
		return nil, fmt.Errorf("provider %s: not supported by provider_factory (agent framework 默认不用 Anthropic 官方；如有需要直接用 internal/llm.NewAnthropic)", name)

	default:
		return nil, fmt.Errorf("provider_factory: unknown provider %q", name)
	}
}

// CreateForVendorModel 根据 vendor model (opus/sonnet/haiku) 创建 provider
//
// 通过 ModelMapping 查到对应的 provider name，再调 CreateProvider。
// vendor model → provider 映射：
//   - opus   → minimax (写作最强, 1M context)
//   - sonnet → deepseek (V4-Pro, 1M context, 自动映射)
//   - haiku  → dashscope (qwen3.7-plus, 1M context)
func (f *ProviderFactory) CreateForVendorModel(mapping *ModelMapping, vendorModel string) (llm.Provider, error) {
	if mapping == nil {
		return nil, fmt.Errorf("provider_factory: nil ModelMapping")
	}
	providerName, _, err := mapping.Map(vendorModel)
	if err != nil {
		return nil, fmt.Errorf("provider_factory: vendor model %q: %w", vendorModel, err)
	}
	return f.CreateProvider(providerName)
}

// CreateAllAvailable 创建所有有 API key 的 provider（便利方法）
//
// 按 vendor model 顺序 (opus/sonnet/haiku) 尝试，每个 vendor model 对应一个 provider。
// 返回 name → Provider 映射。
func (f *ProviderFactory) CreateAllAvailable(mapping *ModelMapping) (map[string]llm.Provider, error) {
	out := make(map[string]llm.Provider)
	for _, vendor := range []string{"opus", "sonnet", "haiku"} {
		p, err := f.CreateForVendorModel(mapping, vendor)
		if err != nil {
			// 缺 key 不报错，跳过（部分 provider 可选）
			continue
		}
		out[vendor] = p
	}
	return out, nil
}

// AvailableProviders 列出当前 factory 可创建的 provider names
// （仅基于 cfg 中是否有 key，不实际创建）
func (f *ProviderFactory) AvailableProviders() []llm.ProviderName {
	var out []llm.ProviderName
	if f.cfg.DashScopeAPIKey != "" {
		out = append(out, llm.ProviderDashScope)
	}
	if f.cfg.DeepSeekAPIKey != "" {
		out = append(out, llm.ProviderDeepSeek)
	}
	if f.cfg.MinimaxAPIKey != "" {
		out = append(out, llm.ProviderMinimax)
	}
	return out
}
