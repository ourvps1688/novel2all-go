package llm

// NewDashScope 阿里云 DashScope（通义千问）— OpenAI 兼容协议
func NewDashScope(apiKey string) Provider {
	return NewOpenAICompat(
		ProviderDashScope,
		apiKey,
		"https://dashscope.aliyuncs.com/compatible-mode",
		DefaultDashScopeModel,
	)
}
