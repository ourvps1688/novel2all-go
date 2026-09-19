package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// TestLLMAPIKeyMiddleware_NoHeaders 测试无 header 时透传 context
func TestLLMAPIKeyMiddleware_NoHeaders(t *testing.T) {
	var capturedKeys llm.APIKeyMap
	handler := LLMAPIKeyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedKeys = llm.APIKeyMap{}
		for _, p := range []llm.ProviderName{llm.ProviderDashScope, llm.ProviderDeepSeek, llm.ProviderMinimax} {
			if k := llm.APIKeyFromContext(r.Context(), p); k != "" {
				capturedKeys[p] = k
			}
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if len(capturedKeys) != 0 {
		t.Errorf("no headers should yield empty keys, got %v", capturedKeys)
	}
}

// TestLLMAPIKeyMiddleware_AllHeaders 测试 3 个 header 都设置
func TestLLMAPIKeyMiddleware_AllHeaders(t *testing.T) {
	var capturedKeys llm.APIKeyMap
	handler := LLMAPIKeyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedKeys = llm.APIKeyMap{}
		for _, p := range []llm.ProviderName{llm.ProviderDashScope, llm.ProviderDeepSeek, llm.ProviderMinimax} {
			if k := llm.APIKeyFromContext(r.Context(), p); k != "" {
				capturedKeys[p] = k
			}
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderLLMKeyDashScope, "sk-dash-test")
	req.Header.Set(HeaderLLMKeyDeepSeek, "sk-deep-test")
	req.Header.Set(HeaderLLMKeyMinimax, "sk-minimax-test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	want := llm.APIKeyMap{
		llm.ProviderDashScope: "sk-dash-test",
		llm.ProviderDeepSeek:  "sk-deep-test",
		llm.ProviderMinimax:   "sk-minimax-test",
	}
	if len(capturedKeys) != len(want) {
		t.Fatalf("captured %d keys, want %d", len(capturedKeys), len(want))
	}
	for k, v := range want {
		if capturedKeys[k] != v {
			t.Errorf("key %q: got %q, want %q", k, capturedKeys[k], v)
		}
	}
}

// TestLLMAPIKeyMiddleware_PartialHeaders 测试部分 header (模拟用户只配置 1 个 provider)
func TestLLMAPIKeyMiddleware_PartialHeaders(t *testing.T) {
	var capturedKeys llm.APIKeyMap
	handler := LLMAPIKeyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedKeys = llm.APIKeyMap{}
		for _, p := range []llm.ProviderName{llm.ProviderDashScope, llm.ProviderDeepSeek, llm.ProviderMinimax} {
			if k := llm.APIKeyFromContext(r.Context(), p); k != "" {
				capturedKeys[p] = k
			}
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderLLMKeyDeepSeek, "sk-deep-only") // 只设 deepseek
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// DashScope 和 Minimax 应为空 (fallback 到 admin env key)
	if v := llm.APIKeyFromContext(req.Context(), llm.ProviderDashScope); v != "" {
		t.Errorf("DashScope should be empty, got %q", v)
	}
	if v := llm.APIKeyFromContext(req.Context(), llm.ProviderMinimax); v != "" {
		t.Errorf("Minimax should be empty, got %q", v)
	}
	// DeepSeek 应被设置
	if capturedKeys[llm.ProviderDeepSeek] != "sk-deep-only" {
		t.Errorf("DeepSeek key not captured: %v", capturedKeys)
	}
}

// TestLLMAPIKeyMiddleware_EmptyHeaderValue 测试 header 设置但值为空 (等价未设)
func TestLLMAPIKeyMiddleware_EmptyHeaderValue(t *testing.T) {
	var ctxHasDashKey bool
	handler := LLMAPIKeyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxHasDashKey = llm.APIKeyFromContext(r.Context(), llm.ProviderDashScope) != ""
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderLLMKeyDashScope, "") // 空字符串
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ctxHasDashKey {
		t.Error("empty header value should not set context key")
	}
}

// TestLLMAPIKeyMiddleware_OriginalRequestUnchanged 测试 middleware 不修改原 Request
// (避免下游 handler 看到 header 注入 → 日志泄露 key)
func TestLLMAPIKeyMiddleware_OriginalRequestUnchanged(t *testing.T) {
	handler := LLMAPIKeyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderLLMKeyMinimax, "sk-secret-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// 原 Request header 应仍包含 user key (r.WithContext 不修改 r 本身)
	if got := req.Header.Get(HeaderLLMKeyMinimax); got != "sk-secret-key" {
		t.Errorf("original request header should be unchanged, got %q", got)
	}
}
