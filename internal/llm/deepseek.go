package llm

// NewDeepSeek DeepSeek 直连 — Anthropic 兼容协议 (Sprint A1.13/A1.22)
//
// 2026-09-18 改造：DeepSeek 走 Anthropic 兼容端点 + 自动 claude-* model 映射
//   - base_url: https://api.deepseek.com/anthropic  (Anthropic 协议)
//   - model: DefaultDeepSeekModel = "claude-opus-4-5-20250929"
//     → DeepSeek 服务端自动映射到 deepseek-v4-pro (V4-Pro, 1M context, $0.66/M input peak)
//   - 备选模型名 claude-haiku-3-5-20241022 → 自动 → deepseek-flash (V4.1-Flash, 1M context, $0.022/M input peak)
//   - deepseek-chat (V3) 已于 2026-09-14 后退役，不要再用
//
// DeepSeek thinking 模式 (A1.23)：
//   - 非 thinking 模式 top_p 固定 1.0
//   - thinking 模式 top_p 下限 0.95
//   - 当前默认走非 thinking，需要时显式 req.Thinking=true 开启
//
// 官方文档：
//   - Anthropic API: https://api-docs.deepseek.com/guides/anthropic_api
//   - Pricing: https://api-docs.deepseek.com/quick_start/pricing
func NewDeepSeek(apiKey string) Provider {
	return NewAnthropicCompat(
		ProviderDeepSeek,
		apiKey,
		"https://api.deepseek.com/anthropic",
		DefaultDeepSeekModel,
	)
}
