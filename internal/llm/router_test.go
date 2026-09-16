package llm

import (
	"testing"
)

func TestNewRouter_NoKeys(t *testing.T) {
	r := NewRouter(LLMConfig{})
	if got := len(r.AvailableProviders()); got != 0 {
		t.Errorf("无 key 时应有 0 个 provider，实际=%d", got)
	}
}

func TestNewRouter_AllKeys(t *testing.T) {
	r := NewRouter(LLMConfig{
		DashScopeAPIKey: "k1",
		DeepSeekAPIKey:  "k2",
		MinimaxAPIKey:   "k3",
		AnthropicAPIKey: "k4",
	})
	if got := len(r.AvailableProviders()); got != 4 {
		t.Errorf("4 个 key 应有 4 个 provider，实际=%d", got)
	}
}

func TestRouter_Resolve_Writing(t *testing.T) {
	r := NewRouter(LLMConfig{
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
	r := NewRouter(LLMConfig{
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
	r := NewRouter(LLMConfig{
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

func TestRouter_Resolve_OverrideProviderNotConfigured(t *testing.T) {
	r := NewRouter(LLMConfig{
		DeepSeekAPIKey: "k2",
	})
	_, _, err := r.resolve(Request{
		OverrideProvider: ProviderAnthropic, // 未配置
	})
	if err == nil {
		t.Error("未配置的 override provider 应该报错")
	}
}

func TestRouter_Resolve_Fallback(t *testing.T) {
	r := NewRouter(LLMConfig{
		DeepSeekAPIKey: "k2",
		// minimax 未配置
	})
	// WRITING 默认走 minimax，应该 fallback 到 deepseek
	p, _, err := r.resolve(Request{Task: TaskWriting})
	if err != nil {
		t.Fatalf("resolve 失败：%v", err)
	}
	if p.Name() != ProviderDeepSeek {
		t.Errorf("minimax 未配置时 WRITING 应 fallback 到 deepseek，实际=%s", p.Name())
	}
}

func TestRouter_Resolve_NoProvider(t *testing.T) {
	r := NewRouter(LLMConfig{}) // 全部未配置
	_, _, err := r.resolve(Request{Task: TaskWriting})
	if err == nil {
		t.Error("全部 provider 未配置应该报错")
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

func TestDashScope_UsesOpenAICompat(t *testing.T) {
	p := NewDashScope("test-key")
	if p.Name() != ProviderDashScope {
		t.Errorf("Name=%s, want=%s", p.Name(), ProviderDashScope)
	}
	if !p.Available() {
		t.Error("Available 应为 true")
	}
	// 类型断言确保是 OpenAI 协议
	if _, ok := p.(*OpenAICompat); !ok {
		t.Error("dashscope 应该是 OpenAI 协议")
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