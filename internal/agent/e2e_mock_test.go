package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// mockAnthropicServer 模拟 Anthropic Messages API（Sprint A1.19 mock 测试）
//
// 记录所有请求（path + headers + body），返回固定的 text 响应。
// 供 agent framework 验证 3 provider 的 wire 协议用。
type mockAnthropicServer struct {
	server   *httptest.Server
	requests []mockRequest
}

type mockRequest struct {
	Path    string
	Headers http.Header
	Body    []byte
}

func newMockAnthropicServer(responseContent string, inputTokens, outputTokens int) *mockAnthropicServer {
	m := &mockAnthropicServer{}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.requests = append(m.requests, mockRequest{
			Path:    r.URL.Path,
			Headers: r.Header.Clone(),
			Body:    body,
		})

		resp := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": responseContent},
			},
			"usage": map[string]int{
				"input_tokens":  inputTokens,
				"output_tokens": outputTokens,
			},
			"model": "mock-model",
			"role":  "assistant",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return m
}

func (m *mockAnthropicServer) Close()                  { m.server.Close() }
func (m *mockAnthropicServer) URL() string             { return m.server.URL }
func (m *mockAnthropicServer) RequestCount() int       { return len(m.requests) }
func (m *mockAnthropicServer) LastRequest() mockRequest { return m.requests[len(m.requests)-1] }

// TestMockE2E_DeepSeekAutoMapping 验证 DeepSeek 发 claude-opus-* 触发自动映射
func TestMockE2E_DeepSeekAutoMapping(t *testing.T) {
	mock := newMockAnthropicServer("Hello from DeepSeek mock", 10, 20)
	defer mock.Close()

	// 用 NewAnthropicCompat + mock URL（NewDeepSeek URL 写死不能换）
	p := llm.NewAnthropicCompat(
		llm.ProviderDeepSeek,
		"fake-deepseek-key",
		mock.URL(),
		llm.DefaultDeepSeekModel, // claude-opus-4-5-20250929
	)

	resp, err := p.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "Hi DeepSeek"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from DeepSeek mock" {
		t.Errorf("Content=%q", resp.Content)
	}
	if mock.RequestCount() != 1 {
		t.Fatalf("请求次数=%d, want 1", mock.RequestCount())
	}

	// 验证请求 path
	last := mock.LastRequest()
	if last.Path != "/v1/messages" {
		t.Errorf("Path=%q, want /v1/messages", last.Path)
	}

	// 验证请求 headers
	if last.Headers.Get("x-api-key") != "fake-deepseek-key" {
		t.Errorf("x-api-key header 错")
	}
	if last.Headers.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version header 错")
	}
	if !strings.Contains(last.Headers.Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type header 错")
	}

	// 验证请求 body
	var body map[string]any
	if err := json.Unmarshal(last.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["model"] != "claude-opus-4-5-20250929" {
		t.Errorf("model=%v, want claude-opus-4-5-20250929 (DeepSeek 自动映射)", body["model"])
	}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Errorf("messages 应有 1 条，实际=%v", body["messages"])
	}
}

// TestMockE2E_DashScopeRealModel 验证千问发真实 qwen3.7-plus（无自动映射）
func TestMockE2E_DashScopeRealModel(t *testing.T) {
	mock := newMockAnthropicServer("Hello from DashScope mock", 15, 25)
	defer mock.Close()

	p := llm.NewAnthropicCompat(
		llm.ProviderDashScope,
		"fake-dashscope-key",
		mock.URL(),
		llm.DefaultDashScopeModel, // qwen3.7-plus
	)

	resp, err := p.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "Hi 千问"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from DashScope mock" {
		t.Errorf("Content=%q", resp.Content)
	}

	last := mock.LastRequest()
	var body map[string]any
	json.Unmarshal(last.Body, &body)

	// 千问必须用真实 model 名，不能是 claude-*
	if body["model"] != "qwen3.7-plus" {
		t.Errorf("model=%v, want qwen3.7-plus（千问真实名）", body["model"])
	}
	if strings.Contains(body["model"].(string), "claude") {
		t.Errorf("千问不能发 claude-*（无自动映射机制），实际=%v", body["model"])
	}
}

// TestMockE2E_MinimaxDefaults 验证 MiniMax 注入专属默认值
func TestMockE2E_MinimaxDefaults(t *testing.T) {
	mock := newMockAnthropicServer("Hello from MiniMax mock", 20, 30)
	defer mock.Close()

	// 用 NewMinimax 构造函数（自动注入 defaults + 国内版 URL）
	p := llm.NewAnthropicCompat(
		llm.ProviderMinimax,
		"fake-minimax-key",
		mock.URL(),
		llm.DefaultMinimaxModel, // MiniMax-M3
	)
	p.UseMinimaxDefaults() // 注入 temperature=1.0, top_p=0.95

	// Request 不显式设置 temperature，期望注入默认值
	resp, err := p.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "Hi MiniMax"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from MiniMax mock" {
		t.Errorf("Content=%q", resp.Content)
	}

	last := mock.LastRequest()
	var body map[string]any
	json.Unmarshal(last.Body, &body)

	if body["model"] != "MiniMax-M3" {
		t.Errorf("model=%v, want MiniMax-M3（国内版）", body["model"])
	}

	// 验证 MiniMax defaults 已注入
	temp, ok := body["temperature"].(float64)
	if !ok || temp != 1.0 {
		t.Errorf("temperature 应被注入 =1.0，实际=%v (类型 %T)", body["temperature"], body["temperature"])
	}
	topP, ok := body["top_p"].(float64)
	if !ok || topP != 0.95 {
		t.Errorf("top_p 应被注入 =0.95，实际=%v (类型 %T)", body["top_p"], body["top_p"])
	}
}

// TestMockE2E_MinimaxExplicitOverridesDefaults 验证 Request 显式 temperature 覆盖 defaults
func TestMockE2E_MinimaxExplicitOverridesDefaults(t *testing.T) {
	mock := newMockAnthropicServer("Hello", 10, 10)
	defer mock.Close()

	p := llm.NewAnthropicCompat(
		llm.ProviderMinimax,
		"fake-key",
		mock.URL(),
		"MiniMax-M3",
	)
	p.UseMinimaxDefaults()

	// 显式设置 temperature=0.5
	_, err := p.Chat(context.Background(), llm.Request{
		Messages:    []llm.Message{{Role: "user", Content: "Hi"}},
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	last := mock.LastRequest()
	var body map[string]any
	json.Unmarshal(last.Body, &body)

	// 显式 temperature 应覆盖 defaults（=0.5，不是 1.0）
	if temp, _ := body["temperature"].(float64); temp != 0.5 {
		t.Errorf("显式 Temperature 应覆盖 defaults，实际=%v", body["temperature"])
	}
}

// TestMockE2E_AllThreeProviders 3 provider 协议统一（端到端 mock）
func TestMockE2E_AllThreeProviders(t *testing.T) {
	mock := newMockAnthropicServer("Unified mock response", 100, 200)
	defer mock.Close()

	providers := []struct {
		name     llm.ProviderName
		model    string
		apiKey   string
		injectMinimaxDefaults bool
	}{
		{llm.ProviderDeepSeek, llm.DefaultDeepSeekModel, "k1", false},
		{llm.ProviderDashScope, llm.DefaultDashScopeModel, "k2", false},
		{llm.ProviderMinimax, llm.DefaultMinimaxModel, "k3", true},
	}

	for _, pp := range providers {
		t.Run(string(pp.name), func(t *testing.T) {
			p := llm.NewAnthropicCompat(pp.name, pp.apiKey, mock.URL(), pp.model)
			if pp.injectMinimaxDefaults {
				p.UseMinimaxDefaults()
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := p.Chat(ctx, llm.Request{
				Messages: []llm.Message{{Role: "user", Content: "test"}},
			})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if resp.Content != "Unified mock response" {
				t.Errorf("Content=%q", resp.Content)
			}
			if resp.TokensIn != 100 || resp.TokensOut != 200 {
				t.Errorf("Tokens in/out 错：%d/%d", resp.TokensIn, resp.TokensOut)
			}
		})
	}
}
