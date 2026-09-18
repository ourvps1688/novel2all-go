package agent

import (
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// ModelMapping vendor model (opus/sonnet/haiku) → Go provider + real model (Sprint A1.8)
//
// vendor oh-story-dsh-0.1.9 的 role .md 用 opus/sonnet/haiku 三个抽象档位。
// Go 端要翻译到具体 provider + real model 名（每个 provider 有自己的 model 命名）。
//
// 2026-09-18 实测官方文档选型（3 个中文 LLM 全部 1M context）：
//   - opus   → MiniMax-M3 (国内版, M 系列最新, 1M context, 写作最强)
//   - sonnet → claude-opus-4-5-20250929 → DeepSeek 自动映射到 deepseek-v4-pro (1M context, V4-Pro 强模型)
//   - haiku  → qwen3.7-plus (1M context, 千问真实 model 名, 千问无 claude-* 自动映射)
//
// 用户可在 configs/agent-models.yaml 覆盖默认映射（决策 6）。
type ModelMapping struct {
	// vendor model → provider + model
	Mapping map[string]MappingEntry
}

// MappingEntry 单个映射条目
type MappingEntry struct {
	Provider llm.ProviderName
	Model    string
}

// DefaultModelMapping 默认模型映射（基于 2026-09-18 决策）
func DefaultModelMapping() *ModelMapping {
	return &ModelMapping{
		Mapping: map[string]MappingEntry{
			"opus": {
				Provider: llm.ProviderMinimax,
				Model:    llm.DefaultMinimaxModel, // MiniMax-M3
			},
			"sonnet": {
				Provider: llm.ProviderDeepSeek,
				Model:    "claude-opus-4-5-20250929", // DeepSeek 自动映射 → deepseek-v4-pro
			},
			"haiku": {
				Provider: llm.ProviderDashScope,
				Model:    "qwen3.7-plus", // 千问真实名（千问无 claude-* 自动映射）
			},
		},
	}
}

// Map 翻译 vendor model → (provider, model)
//
// 找不到的档位返回 error（不要静默 fallback，防止 typo）。
func (m *ModelMapping) Map(vendorModel string) (llm.ProviderName, string, error) {
	if m == nil || m.Mapping == nil {
		return "", "", fmt.Errorf("model mapping: nil")
	}
	entry, ok := m.Mapping[vendorModel]
	if !ok {
		return "", "", fmt.Errorf("model mapping: unknown vendor model %q (must be opus/sonnet/haiku)", vendorModel)
	}
	return entry.Provider, entry.Model, nil
}

// Set 覆盖某个 vendor model 的映射（用于运行时调整）
func (m *ModelMapping) Set(vendorModel string, entry MappingEntry) error {
	switch vendorModel {
	case "opus", "sonnet", "haiku":
		// OK
	default:
		return fmt.Errorf("model mapping: cannot set unknown vendor model %q", vendorModel)
	}
	if m.Mapping == nil {
		m.Mapping = make(map[string]MappingEntry)
	}
	m.Mapping[vendorModel] = entry
	return nil
}

// ProviderFor 只返回 provider（不常用，便利方法）
func (m *ModelMapping) ProviderFor(vendorModel string) (llm.ProviderName, error) {
	p, _, err := m.Map(vendorModel)
	return p, err
}

// ModelFor 只返回 model name（不常用，便利方法）
func (m *ModelMapping) ModelFor(vendorModel string) (string, error) {
	_, model, err := m.Map(vendorModel)
	return model, err
}
