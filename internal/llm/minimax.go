package llm

// NewMinimax minimax M3 — ⚠️ 必须用 Anthropic 兼容协议
//
// 实测发现（2026-09-13）：
//   - minimax OpenAI 兼容端点：不存在 / 404
//   - minimax Anthropic 兼容端点：https://api.minimax.cn/anthropic/v1/messages ✅
//
// 这是 P1-A 最关键的一条，必须保证 minimax 走 AnthropicCompat 基类，不能用 OpenAICompat
func NewMinimax(apiKey string) Provider {
	return NewAnthropicCompat(
		ProviderMinimax,
		apiKey,
		"https://api.minimax.cn/anthropic",
		DefaultMinimaxModel,
	)
}