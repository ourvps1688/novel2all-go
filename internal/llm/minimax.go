package llm

// NewMinimax minimax M3 — ⚠️ 必须用 Anthropic 兼容协议 (Sprint A1.15')
//
// 实测发现（2026-09-13）：
//   - minimax OpenAI 兼容端点：不存在 / 404
//   - minimax Anthropic 兼容端点：https://api.minimax.cn/anthropic/v1/messages ✅
//
// 这是 P1-A 最关键的一条，必须保证 minimax 走 AnthropicCompat 基类，不能用 OpenAICompat
//
// 国内版确认（2026-09-18 用户）：
//   - base_url: https://api.minimax.cn/anthropic  ← **国内版端点**，不是国际版 .io
//   - model: MiniMax-M3  (1M context, M 系列最新, 国内版)
//   - 用户是国内用户，必须用 .cn 端点（.io 国际版不用）
//
// MiniMax-M3 国内版专属默认值（通过 AnthropicCompat.WithMinimaxDefaults 注入）：
//   - temperature: 1.0  (官方推荐值)
//   - top_p:      0.95  (M3 默认值，M2.x 是 0.9)
//   - thinking:   默认关闭，需显式 thinking={"type":"adaptive"} 才启用
//
// 官方文档：https://platform.minimax.cn/docs/api-reference/text-anthropic-api
func NewMinimax(apiKey string) Provider {
	p := NewAnthropicCompat(
		ProviderMinimax,
		apiKey,
		"https://api.minimax.cn/anthropic",
		DefaultMinimaxModel,
	)
	// 注入 MiniMax-M3 专属默认值 (A1.20)
	p.UseMinimaxDefaults()
	return p
}
