package llm

// NewDeepSeek DeepSeek 直连 — OpenAI 兼容协议
func NewDeepSeek(apiKey string) Provider {
	return NewOpenAICompat(
		ProviderDeepSeek,
		apiKey,
		"https://api.deepseek.com/v1",
		DefaultDeepSeekModel,
	)
}