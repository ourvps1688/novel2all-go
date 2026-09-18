package llm

// NewDashScope 阿里云 DashScope（通义千问）— Anthropic 兼容协议 (Sprint A1.14)
//
// 2026-09-18 改造：千问走 Anthropic 兼容端点 + 真实 model 名
//   - base_url: https://dashscope.aliyuncs.com/apps/anthropic  (Anthropic 协议)
//   - model: DefaultDashScopeModel = "qwen3.7-plus"  (千问真实名, 1M context)
//
// ⚠️ 重要：千问 Anthropic 兼容 API **没有** claude-* 自动映射机制！
//   - DeepSeek 会自动把 claude-opus-* → deepseek-v4-pro
//   - 千问没有这个映射，发 claude-opus-* 会失败
//   - 必须用千问真实 model 名：qwen3.7-plus / qwen3.8-max / qwen3.8-flash 等
//
// 推荐 model（按能力档）：
//   - 高能力：qwen3.8-max (1M context, 最强)
//   - 平衡：  qwen3.7-plus (1M context) ← 当前默认
//   - 便宜：  qwen3.8-flash (1M context, 效果接近旗舰)
//   - 历史：  qwen-plus / qwen-flash (1M context, 旧名仍可用)
//   - 小模型：qwen-turbo (128K) / qwen-max (32K, 真正 32K！)
//
// 千问与 Anthropic 官方差异：
//   - temperature 范围 [0, 2)，Anthropic 是 [0.0, 1.0]
//   - 无 /v1/models 列表接口（返回 404）
//   - 推荐用业务空间专属域名 https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/apps/anthropic（更快更稳）
//
// 官方文档：
//   - Anthropic Messages: https://help.aliyun.com/zh/model-studio/anthropic-api-messages
//   - 模型列表: https://help.aliyun.com/zh/model-studio/text-generation-model
func NewDashScope(apiKey string) Provider {
	return NewAnthropicCompat(
		ProviderDashScope,
		apiKey,
		"https://dashscope.aliyuncs.com/apps/anthropic",
		DefaultDashScopeModel,
	)
}
