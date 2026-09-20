package llm

import (
	"context"
	"errors"
)

// API key per-request override (Sprint V1.0.1 Module B.2).
//
// 设计: 通过 context.Context 传递 per-request LLM API key,
// 不污染 Request struct (避免动所有 provider 的构造函数).
//
// 流程:
//  1. API middleware 从 HTTP header 读 X-LLM-Key-{provider} (3 个 provider)
//  2. 写入 context.Value(ctx, apiKeyCtxKey, map[ProviderName]string{...})
//  3. Provider.ChatStream(ctx, ...) 读 context, 如有 user key 则覆盖 constructor key
//
// 安全:
//   - user key 只在 request lifetime 内有效 (context 取消后失效)
//   - 不写日志 (key 通过 %v 格式化可能泄露, helper 不接受格式化)
//   - header 大小写不敏感 (Go http.Header canonical form)
//
// 性能: 每次 LLM 调用查 context (O(1) map lookup), 无磁盘 IO.

// APIKeyMap per-provider API key 集合 (Sprint V1.0.1 Module B.2).
//
// Map key = ProviderName, value = user-provided API key (空字符串无效, 会被 helper 过滤).
// 进程内不持久化, 只在 request lifetime 存在.
type APIKeyMap map[ProviderName]string

// ctxKey 是 internal context key 类型 (避免与其他 package 冲突).
// 永远不导出 — 调用方只能通过 helper 函数访问.
type ctxKey int

const (
	apiKeyCtxKey ctxKey = iota
)

// ContextWithAPIKeys 把 per-request API key 写入 context.
//
// keys 中空字符串 value 会被忽略 (不会覆盖 constructor key).
// 返回新 context, 不修改原 ctx.
func ContextWithAPIKeys(ctx context.Context, keys APIKeyMap) context.Context {
	if keys == nil {
		return ctx
	}
	// 过滤空字符串避免后续判断
	cleaned := make(APIKeyMap, len(keys))
	for k, v := range keys {
		if v != "" {
			cleaned[k] = v
		}
	}
	if len(cleaned) == 0 {
		return ctx
	}
	return context.WithValue(ctx, apiKeyCtxKey, cleaned)
}

// APIKeyFromContext 返回指定 provider 的 user-provided API key.
//
// 返回 "" 表示:
//   - context 中没有 user key (fall back to constructor key)
//   - 该 provider 没设置 user key (fall back to constructor key)
//
// 调用方应这样使用:
//
//	key := llm.APIKeyFromContext(ctx, p.Name())
//	if key == "" {
//	    key = p.apiKey  // fallback to constructor
//	}
func APIKeyFromContext(ctx context.Context, provider ProviderName) string {
	if ctx == nil {
		return ""
	}
	m, ok := ctx.Value(apiKeyCtxKey).(APIKeyMap)
	if !ok {
		return ""
	}
	return m[provider]
}

// ErrNoAPIKey Sprint V1.0.1 (2026-09-20): 所有 LLM 调用都需要 user-supplied key
// (从 X-LLM-Key-{provider} header). Admin key fallback 默认禁用 (Module I 决策).
//
// 当 handler 准备好调用 LLM 但拿不到任何 key 时, 应该立即返这个错误 (503),
// 不要让请求到达上游 (避免不必要的 network roundtrip + 给用户明确提示).
var ErrNoAPIKey = errors.New("user API key required: please configure your API key in SettingsPage (the server has no admin key fallback)")

// ResolveAPIKey 综合解析 user key (ctx) + admin key (constructor) 用于指定 provider.
// 返回顺序 (Module B.2 + Module I 决策):
//  1. user-supplied key (from X-LLM-Key-{provider} header via ctx)
//  2. admin key (from constructor, 仅当 LLM_ALLOW_ADMIN_FALLBACK=true 时非空)
//  3. 否则返回 ErrNoAPIKey
//
// 调用方应该:
//
//	key, err := llm.ResolveAPIKey(ctx, p.Name(), p.apiKey)
//	if err != nil { return err }  // HTTP handler 返 503
//
// pName: provider 标识 (e.g. "dashscope")
// constructorKey: provider 初始化时存的 admin key (Sprint V1.0.1 默认空字符串)
func ResolveAPIKey(ctx context.Context, provider ProviderName, constructorKey string) (string, error) {
	if userKey := APIKeyFromContext(ctx, provider); userKey != "" {
		return userKey, nil
	}
	if constructorKey != "" {
		return constructorKey, nil
	}
	return "", ErrNoAPIKey
}

// SetAPIKeyOnContext 是 ContextWithAPIKeys 的单 key 便捷版本.
//
// 用于单个 provider 场景 (如 middleware 知道只覆盖一个 provider).
func SetAPIKeyOnContext(ctx context.Context, provider ProviderName, key string) context.Context {
	if key == "" {
		return ctx
	}
	// 合并现有 map (避免覆盖其他 provider 的 user key)
	merged := APIKeyMap{provider: key}
	if existing, ok := ctx.Value(apiKeyCtxKey).(APIKeyMap); ok {
		for k, v := range existing {
			if _, dup := merged[k]; !dup {
				merged[k] = v
			}
		}
	}
	return context.WithValue(ctx, apiKeyCtxKey, merged)
}
