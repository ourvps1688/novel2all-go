package llm

import (
	"testing"
)

func TestNewRouter_NoKeys(t *testing.T) {
	// Sprint V1.0.1 (2026-09-20): 无条件注册所有 4 个 provider (即使 apiKey 空).
	// router.resolve 的 fallback 检查 Available() (key 非空). 这样 resolve 不报错,
	// 后续 Chat 的早期检查触发 ErrNoAPIKey (友好提示).
	r := NewRouter(Config{})
	if got := len(r.providers); got != 4 {
		t.Errorf("无条件注册 4 个 provider, 实际=%d", got)
	}
	// 但 Available() 应返 0 (所有 key 都空)
	if got := len(r.AvailableProviders()); got != 0 {
		t.Errorf("无 key 时应有 0 个 Available provider, 实际=%d", got)
	}
}

func TestNewRouter_AllKeys(t *testing.T) {
	r := NewRouter(Config{
		DashScopeAPIKey: "k1",
		DeepSeekAPIKey:  "k2",
		MinimaxAPIKey:   "k3",
		AnthropicAPIKey: "k4",
	})
	if got := len(r.providers); got != 4 {
		t.Errorf("4 个 key 应有 4 个 provider，实际=%d", got)
	}
	if got := len(r.AvailableProviders()); got != 4 {
		t.Errorf("4 个 key 应有 4 个 Available provider，实际=%d", got)
	}
}

func TestRouter_Resolve_Writing(t *testing.T) {
	r := NewRouter(Config{
		DeepSeekAPIKey: "k2",
		MinimaxAPIKey:  "k3",
	})
	p, model, err := r.resolve(Request{Task: TaskWriting})
	if err != nil {
		t.Fatalf("resolve 失败：%v", err)
	}
	if p.Name() != ProviderMinimax {
		t.Errorf("WRITING 应路由到 minimax，实际=%s", p.Name())
	}
	if model != DefaultMinimaxModel {
		t.Errorf("WRITING 模型应=%s，实际=%s", DefaultMinimaxModel, model)
	}
}

func TestRouter_Resolve_Consistency(t *testing.T) {
	r := NewRouter(Config{
		DeepSeekAPIKey: "k2",
		MinimaxAPIKey:  "k3",
	})
	p, _, err := r.resolve(Request{Task: TaskConsistency})
	if err != nil {
		t.Fatalf("resolve 失败：%v", err)
	}
	if p.Name() != ProviderDeepSeek {
		t.Errorf("CONSISTENCY 应路由到 deepseek，实际=%s", p.Name())
	}
}

func TestRouter_Resolve_OverrideProvider(t *testing.T) {
	r := NewRouter(Config{
		DashScopeAPIKey: "k1",
		DeepSeekAPIKey:  "k2",
	})
	p, _, err := r.resolve(Request{
		Task:             TaskWriting, // 默认走 minimax
		OverrideProvider: ProviderDashScope,
	})
	if err != nil {
		t.Fatalf("resolve 失败：%v", err)
	}
	if p.Name() != ProviderDashScope {
		t.Errorf("override 应强制 dashscope，实际=%s", p.Name())
	}
}

// Sprint V1.0.1 (2026-09-20): override 到未配置的 provider 现在返回该 provider
// (Available=false). 后续 Chat 早期检查会触发 ErrNoAPIKey (友好提示).
func TestRouter_Resolve_OverrideProviderNotConfigured(t *testing.T) {
	r := NewRouter(Config{
		DeepSeekAPIKey: "k2",
	})
	p, _, err := r.resolve(Request{
		OverrideProvider: ProviderAnthropic, // 未配置 key, 但 provider 已注册
	})
	if err != nil {
		t.Fatalf("override 到未配置 provider 不应 resolve 报错: %v", err)
	}
	if p.Name() != ProviderAnthropic {
		t.Errorf("override 应返回 anthropic provider, 实际=%s", p.Name())
	}
	if p.Available() {
		t.Error("anthropic 未配置 key, Available 应返 false")
	}
}

// Sprint V1.0.1 (2026-09-20): fallback 逻辑也检查 Available().
// minimax 未配置 (key 空) → fallback 到 deepseek (key 有).
func TestRouter_Resolve_Fallback(t *testing.T) {
	r := NewRouter(Config{
		DeepSeekAPIKey: "k2",
		// minimax 未配置 (key 空)
	})
	// WRITING 默认走 minimax，但 minimax Available=false → fallback 到 deepseek
	p, _, err := r.resolve(Request{Task: TaskWriting})
	if err != nil {
		t.Fatalf("resolve 失败：%v", err)
	}
	if p.Name() != ProviderDeepSeek {
		t.Errorf("minimax Available=false 时 WRITING 应 fallback 到 deepseek，实际=%s", p.Name())
	}
}

// Sprint V1.0.1 (2026-09-20): 全部未配置时 resolve 不再报错, 而是返主 provider
// (即使 Available=false). 后续 Chat 检查会触发 ErrNoAPIKey.
func TestRouter_Resolve_NoProvider(t *testing.T) {
	r := NewRouter(Config{}) // 全部未配置
	p, _, err := r.resolve(Request{Task: TaskWriting})
	if err != nil {
		t.Fatalf("resolve 不应报错 (后续 Chat 检查 ErrNoAPIKey): %v", err)
	}
	if p == nil {
		t.Fatal("resolve 应返回 provider (即使 Available=false)")
	}
	// 主 provider WRITING → minimax. 即使空 key 也返 minimax (后续 Chat 检查)
	if p.Name() != ProviderMinimax {
		t.Errorf("WRITING 应路由 minimax, 实际=%s", p.Name())
	}
	if p.Available() {
		t.Error("minimax 未配置 key, Available 应返 false")
	}
}

func TestDefaultRoutes(t *testing.T) {
	rts := defaultRoutes()
	if rts[TaskWriting].Provider != ProviderMinimax {
		t.Error("WRITING 应默认 minimax")
	}
	for _, task := range []TaskType{TaskConsistency, TaskExtraction, TaskSummarization, TaskCover, TaskUnknown} {
		if rts[task].Provider != ProviderDeepSeek {
			t.Errorf("%s 应默认 deepseek，实际=%s", task, rts[task].Provider)
		}
	}
}

func TestDashScope_UsesAnthropicCompat(t *testing.T) {
	p := NewDashScope("test-key")
	if p.Name() != ProviderDashScope {
		t.Errorf("Name=%s, want=%s", p.Name(), ProviderDashScope)
	}
	if !p.Available() {
		t.Error("Available 应为 true")
	}
	// Sprint A1.14: dashscope 已切到 Anthropic 协议
	if _, ok := p.(*AnthropicCompat); !ok {
		t.Error("dashscope 应该是 Anthropic 协议（Sprint A1.14）")
	}
}

func TestDeepSeek_UsesAnthropicCompat(t *testing.T) {
	p := NewDeepSeek("test-key")
	if p.Name() != ProviderDeepSeek {
		t.Errorf("Name=%s, want=%s", p.Name(), ProviderDeepSeek)
	}
	if !p.Available() {
		t.Error("Available 应为 true")
	}
	// Sprint A1.13: deepseek 已切到 Anthropic 协议
	if _, ok := p.(*AnthropicCompat); !ok {
		t.Error("deepseek 应该是 Anthropic 协议（Sprint A1.13）")
	}
}

func TestMinimax_HasDefaults(t *testing.T) {
	p := NewMinimax("test-key")
	if _, ok := p.(*AnthropicCompat); !ok {
		t.Fatal("minimax 应该是 Anthropic 协议")
	}
	// Sprint A1.20: minimax 应注入 MiniMax-M3 专属 defaults
	ac, ok := p.(*AnthropicCompat)
	if !ok {
		t.Fatal("minimax 应该是 AnthropicCompat 类型")
	}
	if !ac.HasMinimaxDefaults() {
		t.Error("minimax 应自动注入 MiniMax-M3 defaults (temperature=1.0, top_p=0.95)")
	}
}

func TestDashScope_NoMinimaxDefaults(t *testing.T) {
	p := NewDashScope("test-key")
	ac, ok := p.(*AnthropicCompat)
	if !ok {
		t.Fatal("dashscope 应该是 AnthropicCompat 类型")
	}
	// Sprint A1.20: dashscope 不注入 MiniMax defaults
	if ac.HasMinimaxDefaults() {
		t.Error("dashscope 不应注入 MiniMax defaults（仅 minimax 注入）")
	}
}

func TestMinimax_UsesAnthropicCompat(t *testing.T) {
	p := NewMinimax("test-key")
	if p.Name() != ProviderMinimax {
		t.Errorf("Name=%s, want=%s", p.Name(), ProviderMinimax)
	}
	// 关键！minimax 必须是 Anthropic 协议
	if _, ok := p.(*AnthropicCompat); !ok {
		t.Error("minimax 必须是 Anthropic 协议（实测 2026-09-13：OpenAI 端点 404）")
	}
}

func TestAnthropic_UsesAnthropicCompat(t *testing.T) {
	p := NewAnthropic("test-key")
	if _, ok := p.(*AnthropicCompat); !ok {
		t.Error("anthropic 应该是 Anthropic 协议")
	}
}

func TestProvider_Available(t *testing.T) {
	if (&OpenAICompat{apiKey: ""}).Available() {
		t.Error("空 apiKey 应 Available=false")
	}
	if !(&OpenAICompat{apiKey: "k"}).Available() {
		t.Error("非空 apiKey 应 Available=true")
	}
}
