package agent

import (
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

func TestProviderFactory_NewProviderFactory(t *testing.T) {
	f := NewProviderFactory(llm.Config{
		DeepSeekAPIKey:  "k1",
		DashScopeAPIKey: "k2",
		MinimaxAPIKey:   "k3",
	})
	if f == nil {
		t.Fatal("NewProviderFactory 返回 nil")
	}
}

func TestProviderFactory_CreateProvider_AllThree(t *testing.T) {
	f := NewProviderFactory(llm.Config{
		DashScopeAPIKey: "k-dash",
		DeepSeekAPIKey:  "k-deep",
		MinimaxAPIKey:   "k-mini",
	})

	tests := []struct {
		name     llm.ProviderName
		keyField string
	}{
		{llm.ProviderDashScope, "k-dash"},
		{llm.ProviderDeepSeek, "k-deep"},
		{llm.ProviderMinimax, "k-mini"},
	}
	for _, tt := range tests {
		p, err := f.CreateProvider(tt.name)
		if err != nil {
			t.Errorf("CreateProvider(%s): %v", tt.name, err)
			continue
		}
		if p == nil {
			t.Errorf("CreateProvider(%s): nil provider", tt.name)
			continue
		}
		if p.Name() != tt.name {
			t.Errorf("CreateProvider(%s): Name()=%s", tt.name, p.Name())
		}
		if !p.Available() {
			t.Errorf("CreateProvider(%s): Available()=false", tt.name)
		}
	}
}

func TestProviderFactory_CreateProvider_MissingKey(t *testing.T) {
	f := NewProviderFactory(llm.Config{}) // 全部缺 key
	tests := []llm.ProviderName{
		llm.ProviderDashScope,
		llm.ProviderDeepSeek,
		llm.ProviderMinimax,
	}
	for _, name := range tests {
		_, err := f.CreateProvider(name)
		if err == nil {
			t.Errorf("CreateProvider(%s) 缺 key 应报错", name)
		}
		if !strings.Contains(err.Error(), "not configured") {
			t.Errorf("error 应含 'not configured'，实际=%q", err.Error())
		}
	}
}

func TestProviderFactory_CreateProvider_AnthropicNotSupported(t *testing.T) {
	f := NewProviderFactory(llm.Config{AnthropicAPIKey: "k"})
	_, err := f.CreateProvider(llm.ProviderAnthropic)
	if err == nil {
		t.Error("Anthropic 官方 provider 应不被 factory 支持")
	}
}

func TestProviderFactory_CreateProvider_Unknown(t *testing.T) {
	f := NewProviderFactory(llm.Config{})
	_, err := f.CreateProvider(llm.ProviderName("nonexistent"))
	if err == nil {
		t.Error("unknown provider 应报错")
	}
}

func TestProviderFactory_CreateForVendorModel(t *testing.T) {
	f := NewProviderFactory(llm.Config{
		DashScopeAPIKey: "k-dash",
		DeepSeekAPIKey:  "k-deep",
		MinimaxAPIKey:   "k-mini",
	})
	m := DefaultModelMapping()

	tests := []struct {
		vendor string
		want   llm.ProviderName
	}{
		{"opus", llm.ProviderMinimax},
		{"sonnet", llm.ProviderDeepSeek},
		{"haiku", llm.ProviderDashScope},
	}
	for _, tt := range tests {
		p, err := f.CreateForVendorModel(m, tt.vendor)
		if err != nil {
			t.Errorf("CreateForVendorModel(%s): %v", tt.vendor, err)
			continue
		}
		if p.Name() != tt.want {
			t.Errorf("CreateForVendorModel(%s) → %s, want %s", tt.vendor, p.Name(), tt.want)
		}
	}
}

func TestProviderFactory_CreateForVendorModel_InvalidVendor(t *testing.T) {
	f := NewProviderFactory(llm.Config{MinimaxAPIKey: "k"})
	m := DefaultModelMapping()

	_, err := f.CreateForVendorModel(m, "gpt-5")
	if err == nil {
		t.Error("unknown vendor model 应报错")
	}
}

func TestProviderFactory_CreateForVendorModel_MissingKey(t *testing.T) {
	// 只配 minimax，opus 需要 minimax，所以 opus 能创建
	// sonnet 需要 deepseek，缺 key → 报错
	f := NewProviderFactory(llm.Config{MinimaxAPIKey: "k"})
	m := DefaultModelMapping()

	// opus 应该成功（用 minimax）
	_, err := f.CreateForVendorModel(m, "opus")
	if err != nil {
		t.Errorf("opus 应成功（minimax 已配）：%v", err)
	}

	// sonnet 应失败（deepseek 缺 key）
	_, err = f.CreateForVendorModel(m, "sonnet")
	if err == nil {
		t.Error("sonnet 应失败（deepseek 缺 key）")
	}
}

func TestProviderFactory_CreateForVendorModel_NilMapping(t *testing.T) {
	f := NewProviderFactory(llm.Config{MinimaxAPIKey: "k"})
	_, err := f.CreateForVendorModel(nil, "opus")
	if err == nil {
		t.Error("nil ModelMapping 应报错")
	}
}

func TestProviderFactory_CreateAllAvailable(t *testing.T) {
	f := NewProviderFactory(llm.Config{
		DashScopeAPIKey: "k-dash",
		DeepSeekAPIKey:  "k-deep",
		// MinimaxAPIKey 缺
	})
	m := DefaultModelMapping()

	all, err := f.CreateAllAvailable(m)
	if err != nil {
		t.Fatalf("CreateAllAvailable: %v", err)
	}
	// opus (minimax) 应缺，sonnet (deepseek) + haiku (dashscope) 应有
	if _, ok := all["opus"]; ok {
		t.Error("opus 应缺（minimax 未配）")
	}
	if _, ok := all["sonnet"]; !ok {
		t.Error("sonnet 应有")
	}
	if _, ok := all["haiku"]; !ok {
		t.Error("haiku 应有")
	}
}

func TestProviderFactory_AvailableProviders(t *testing.T) {
	tests := []struct {
		name string
		cfg  llm.Config
		want []llm.ProviderName
	}{
		{
			"all",
			llm.Config{DashScopeAPIKey: "k1", DeepSeekAPIKey: "k2", MinimaxAPIKey: "k3"},
			[]llm.ProviderName{llm.ProviderDashScope, llm.ProviderDeepSeek, llm.ProviderMinimax},
		},
		{
			"only deepseek",
			llm.Config{DeepSeekAPIKey: "k2"},
			[]llm.ProviderName{llm.ProviderDeepSeek},
		},
		{
			"none",
			llm.Config{},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewProviderFactory(tt.cfg)
			got := f.AvailableProviders()
			if len(got) != len(tt.want) {
				t.Errorf("AvailableProviders() 长度=%d, want %d (got %v)", len(got), len(tt.want), got)
			}
		})
	}
}

func TestProviderFactory_MinimaxHasDefaults(t *testing.T) {
	f := NewProviderFactory(llm.Config{MinimaxAPIKey: "k"})
	p, err := f.CreateProvider(llm.ProviderMinimax)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	ac, ok := p.(*llm.AnthropicCompat)
	if !ok {
		t.Fatal("minimax 应是 AnthropicCompat")
	}
	if !ac.HasMinimaxDefaults() {
		t.Error("minimax 应自动注入 MiniMax defaults (via NewMinimax)")
	}
}

func TestProviderFactory_NonMinimaxNoDefaults(t *testing.T) {
	f := NewProviderFactory(llm.Config{
		DashScopeAPIKey: "k1",
		DeepSeekAPIKey:  "k2",
	})
	for _, name := range []llm.ProviderName{llm.ProviderDashScope, llm.ProviderDeepSeek} {
		p, _ := f.CreateProvider(name)
		ac, ok := p.(*llm.AnthropicCompat)
		if !ok {
			t.Fatalf("%s 应是 AnthropicCompat", name)
		}
		if ac.HasMinimaxDefaults() {
			t.Errorf("%s 不应注入 MiniMax defaults", name)
		}
	}
}
