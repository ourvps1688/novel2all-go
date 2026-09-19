package llm

import (
	"context"
	"testing"
)

// TestAPIKeyFromContext_Empty 测试无 context 时返回空
func TestAPIKeyFromContext_Empty(t *testing.T) {
	if k := APIKeyFromContext(context.Background(), ProviderDashScope); k != "" {
		t.Errorf("expected empty key, got %q", k)
	}
	if k := APIKeyFromContext(nil, ProviderDashScope); k != "" {
		t.Errorf("nil ctx should return empty, got %q", k)
	}
}

// TestContextWithAPIKeys_SetAndGet 测试写入 + 读取
func TestContextWithAPIKeys_SetAndGet(t *testing.T) {
	keys := APIKeyMap{
		ProviderDashScope: "sk-dashscope-user",
		ProviderDeepSeek:  "sk-deepseek-user",
	}
	ctx := ContextWithAPIKeys(context.Background(), keys)

	if got := APIKeyFromContext(ctx, ProviderDashScope); got != "sk-dashscope-user" {
		t.Errorf("DashScope key: got %q, want %q", got, "sk-dashscope-user")
	}
	if got := APIKeyFromContext(ctx, ProviderDeepSeek); got != "sk-deepseek-user" {
		t.Errorf("DeepSeek key: got %q, want %q", got, "sk-deepseek-user")
	}
	// minimax 没设置, 应返回空 (fallback)
	if got := APIKeyFromContext(ctx, ProviderMinimax); got != "" {
		t.Errorf("Minimax (unset) key: got %q, want empty", got)
	}
}

// TestContextWithAPIKeys_FilterEmpty 测试空字符串被过滤
func TestContextWithAPIKeys_FilterEmpty(t *testing.T) {
	keys := APIKeyMap{
		ProviderDashScope: "", // 空, 应忽略
		ProviderDeepSeek:  "sk-deepseek",
	}
	ctx := ContextWithAPIKeys(context.Background(), keys)

	if got := APIKeyFromContext(ctx, ProviderDashScope); got != "" {
		t.Errorf("DashScope (empty) should not be set, got %q", got)
	}
	if got := APIKeyFromContext(ctx, ProviderDeepSeek); got != "sk-deepseek" {
		t.Errorf("DeepSeek: got %q, want sk-deepseek", got)
	}
}

// TestContextWithAPIKeys_EmptyMap 测试空 map 不写 context
func TestContextWithAPIKeys_EmptyMap(t *testing.T) {
	ctx1 := context.Background()
	ctx2 := ContextWithAPIKeys(ctx1, nil)
	ctx3 := ContextWithAPIKeys(ctx1, APIKeyMap{})
	ctx4 := ContextWithAPIKeys(ctx1, APIKeyMap{ProviderDashScope: ""})

	// ctx 身份应保持不变 (背景 ctx 等价性)
	if ctx2 != ctx1 {
		t.Error("nil map should return original context")
	}
	if got := APIKeyFromContext(ctx3, ProviderDashScope); got != "" {
		t.Error("empty map should not set anything")
	}
	if got := APIKeyFromContext(ctx4, ProviderDashScope); got != "" {
		t.Error("map with only empty values should not set anything")
	}
}

// TestSetAPIKeyOnContext_Merge 测试单 key 设置合并现有 map
func TestSetAPIKeyOnContext_Merge(t *testing.T) {
	// 初始有 DashScope + DeepSeek
	ctx := ContextWithAPIKeys(context.Background(), APIKeyMap{
		ProviderDashScope: "sk-dash-original",
		ProviderDeepSeek:  "sk-deep-original",
	})

	// 加 minimax
	ctx = SetAPIKeyOnContext(ctx, ProviderMinimax, "sk-minimax")
	if got := APIKeyFromContext(ctx, ProviderMinimax); got != "sk-minimax" {
		t.Errorf("Minimax not added: got %q", got)
	}
	// 原有应保留
	if got := APIKeyFromContext(ctx, ProviderDashScope); got != "sk-dash-original" {
		t.Errorf("DashScope lost: got %q", got)
	}

	// 覆盖 minimax
	ctx = SetAPIKeyOnContext(ctx, ProviderMinimax, "sk-minimax-new")
	if got := APIKeyFromContext(ctx, ProviderMinimax); got != "sk-minimax-new" {
		t.Errorf("Minimax not overridden: got %q", got)
	}
	// 其他不变
	if got := APIKeyFromContext(ctx, ProviderDeepSeek); got != "sk-deep-original" {
		t.Errorf("DeepSeek changed unexpectedly: got %q", got)
	}
}

// TestSetAPIKeyOnContext_EmptyKey 测试空 key 不写
func TestSetAPIKeyOnContext_EmptyKey(t *testing.T) {
	ctx := ContextWithAPIKeys(context.Background(), APIKeyMap{
		ProviderDashScope: "sk-original",
	})
	ctx2 := SetAPIKeyOnContext(ctx, ProviderDashScope, "")
	// 应保持原值
	if got := APIKeyFromContext(ctx2, ProviderDashScope); got != "sk-original" {
		t.Errorf("empty key should not override, got %q", got)
	}
}
