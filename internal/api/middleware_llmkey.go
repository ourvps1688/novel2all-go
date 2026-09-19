package api

import (
	"net/http"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// Per-request LLM API key HTTP header names (Sprint V1.0.1 Module B.2).
//
// 桌面 app (Module B 加密存的 user keys) 在请求时设置这些 header,
// 后端 middleware 读到后塞进 context, LLM provider 自动覆盖 admin env key.
//
// 优先级: header > 环境变量 admin key > 401/403 报错.
const (
	HeaderLLMKeyDashScope = "X-LLM-Key-DashScope"
	HeaderLLMKeyDeepSeek  = "X-LLM-Key-DeepSeek"
	HeaderLLMKeyMinimax   = "X-LLM-Key-Minimax"
)

// LLMAPIKeyMiddleware 读 X-LLM-Key-* header, 透传给 LLM provider.
//
// 用法: mux.Handle("/api/chapter/", llmKeyMiddleware(chapterHandler))
//
// 安全:
//   - header 不写日志 (HTTP middleware 默认 log, 我们覆盖 SilentHeader log)
//   - 仅读取 .Get() 不 mutate header (下游 handler 看不到 header 注入)
//   - 失败降级: header 缺失/空 → 继续处理 (用 admin key fallback), 不阻断
func LLMAPIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 收集所有非空 header (避免覆盖 fallback key)
		keys := make(llm.APIKeyMap, 3)

		if v := r.Header.Get(HeaderLLMKeyDashScope); v != "" {
			keys[llm.ProviderDashScope] = v
		}
		if v := r.Header.Get(HeaderLLMKeyDeepSeek); v != "" {
			keys[llm.ProviderDeepSeek] = v
		}
		if v := r.Header.Get(HeaderLLMKeyMinimax); v != "" {
			keys[llm.ProviderMinimax] = v
		}

		// 即使 keys 为空也要透传 ctx (下游可用 ContextWithAPIKeys 拿空 map)
		ctx := llm.ContextWithAPIKeys(r.Context(), keys)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
