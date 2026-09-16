package llm

// NewAnthropic Anthropic 官方（Claude）— Anthropic 兼容协议
func NewAnthropic(apiKey string) Provider {
	return NewAnthropicCompat(
		ProviderAnthropic,
		apiKey,
		"https://api.anthropic.com",
		DefaultAnthropicModel,
	)
}
