package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// loadTestEnv 加载 configs/.env 文件（如果存在），让真实 E2E 测试拿到 API key
//
// 默认行为：silent 失败（不报错），让 env var 直接控制 skip。
// 测试自身用 os.Getenv("DEEPSEEK_API_KEY") 等检查是否跳过。
func loadTestEnv(t *testing.T) {
	t.Helper()
	// 项目自带 loadEnvFile 是 private，我们简单用 os.Setenv 模拟
	// 实际项目用 internal/config.Load()，但 agent 包不应强依赖 config 包
	// 这里直接读 .env（minimal parser）
	data, err := os.ReadFile("../../configs/.env")
	if err != nil {
		return // .env 不存在没关系
	}
	parseAndSetEnv(string(data))
}

func parseAndSetEnv(content string) {
	for _, line := range splitLines(content) {
		line = trimSpace(line)
		if line == "" || startsWith(line, "#") {
			continue
		}
		eq := indexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := trimSpace(line[:eq])
		value := trimSpace(line[eq+1:])
		// 移除尾部注释
		if hashIdx := indexByte(value, '#'); hashIdx >= 0 {
			value = trimSpace(value[:hashIdx])
		}
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

// 极简字符串工具（避免引入 strings 包依赖，已有依赖可换）
func splitLines(s string) []string {
	var out []string
	var cur []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, string(cur))
			cur = cur[:0]
		} else {
			cur = append(cur, s[i])
		}
	}
	out = append(out, string(cur))
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// TestRealE2E_DeepSeek 用真实 API key 测 DeepSeek (Sprint A1.19 / A6.10)
//
// 运行条件：
//   - configs/.env 含 DEEPSEEK_API_KEY
//   - 不在 -short 模式
func TestRealE2E_DeepSeek(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real LLM E2E in -short mode")
	}
	loadTestEnv(t)

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set (skipping real E2E)")
	}

	f := NewProviderFactory(llm.Config{DeepSeekAPIKey: apiKey})
	p, err := f.CreateProvider(llm.ProviderDeepSeek)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := p.Chat(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "你是一个简洁的助手。"},
			{Role: "user", Content: "用一句话介绍你自己。"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Chat 失败（可能是网络/API 限制）：%v", err)
	}

	if resp.Content == "" {
		t.Error("Content 应非空")
	}
	if resp.TokensIn == 0 && resp.TokensOut == 0 {
		t.Error("Tokens 应 > 0")
	}
	t.Logf("✅ DeepSeek 真实 E2E: Content=%q, Tokens in/out=%d/%d", resp.Content, resp.TokensIn, resp.TokensOut)
}

// TestRealE2E_Minimax 用真实 API key 测 MiniMax (Sprint A1.19 / A6.11)
func TestRealE2E_Minimax(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real LLM E2E in -short mode")
	}
	loadTestEnv(t)

	apiKey := os.Getenv("MINIMAX_API_KEY")
	if apiKey == "" {
		t.Skip("MINIMAX_API_KEY not set (skipping real E2E)")
	}

	f := NewProviderFactory(llm.Config{MinimaxAPIKey: apiKey})
	p, err := f.CreateProvider(llm.ProviderMinimax)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := p.Chat(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "你是一个简洁的助手。"},
			{Role: "user", Content: "用一句话介绍你自己。"},
		},
		MaxTokens: 200,
	})
	if err != nil {
		t.Fatalf("Chat 失败（可能是网络/API 限制）：%v", err)
	}

	if resp.Content == "" {
		t.Error("Content 应非空")
	}
	t.Logf("✅ MiniMax 真实 E2E: Content=%q, Tokens in/out=%d/%d", resp.Content, resp.TokensIn, resp.TokensOut)
}

// TestRealE2E_DashScope 用真实 API key 测千问 (Sprint A6.9)
func TestRealE2E_DashScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real LLM E2E in -short mode")
	}
	loadTestEnv(t)

	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY not set (skipping real E2E)")
	}

	f := NewProviderFactory(llm.Config{DashScopeAPIKey: apiKey})
	p, err := f.CreateProvider(llm.ProviderDashScope)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := p.Chat(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "你是一个简洁的助手。"},
			{Role: "user", Content: "用一句话介绍你自己。"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Chat 失败（可能是网络/API 限制）：%v", err)
	}

	if resp.Content == "" {
		t.Error("Content 应非空")
	}
	t.Logf("✅ DashScope 真实 E2E: Content=%q, Tokens in/out=%d/%d", resp.Content, resp.TokensIn, resp.TokensOut)
}

// TestRealE2E_Factory_AllAvailable 测所有可用的真实 provider
func TestRealE2E_Factory_AllAvailable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real LLM E2E in -short mode")
	}
	loadTestEnv(t)

	cfg := llm.Config{
		DashScopeAPIKey: os.Getenv("DASHSCOPE_API_KEY"),
		DeepSeekAPIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		MinimaxAPIKey:   os.Getenv("MINIMAX_API_KEY"),
	}

	f := NewProviderFactory(cfg)
	available := f.AvailableProviders()
	if len(available) == 0 {
		t.Skip("没有可用的 API key")
	}

	t.Logf("可用 providers: %v", available)

	m := DefaultModelMapping()
	all, err := f.CreateAllAvailable(m)
	if err != nil {
		t.Fatalf("CreateAllAvailable: %v", err)
	}

	for vendor, p := range all {
		t.Run("vendor="+vendor, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			resp, err := p.Chat(ctx, llm.Request{
				Messages: []llm.Message{{Role: "user", Content: "ping"}},
				MaxTokens: 50,
			})
			if err != nil {
				t.Errorf("Chat 失败：%v", err)
				return
			}
			if resp.Content == "" {
				t.Error("Content 空")
			}
			t.Logf("✅ %s: %q (Tokens: %d/%d)", vendor, resp.Content, resp.TokensIn, resp.TokensOut)
		})
	}
}
