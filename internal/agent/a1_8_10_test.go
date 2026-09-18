package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// 提取重复字面量为常量（goconst 3+ 出现要求）
const expectedHaikuModel = "qwen3.7-plus"

func TestDefaultModelMapping(t *testing.T) {
	m := DefaultModelMapping()
	if m == nil {
		t.Fatal("DefaultModelMapping 返回 nil")
	}
	if len(m.Mapping) != 3 {
		t.Errorf("应有 3 个映射（opus/sonnet/haiku），实际=%d", len(m.Mapping))
	}

	// 验证 3 个 vendor model 都映射
	for _, vendor := range []string{"opus", "sonnet", "haiku"} {
		if _, ok := m.Mapping[vendor]; !ok {
			t.Errorf("vendor model %q 缺失", vendor)
		}
	}

	// 验证 2026-09-18 选型
	if p, model, _ := m.Map("opus"); p != llm.ProviderMinimax || model != "MiniMax-M3" {
		t.Errorf("opus 应映射到 minimax/MiniMax-M3，实际=%s/%s", p, model)
	}
	if p, model, _ := m.Map("sonnet"); p != llm.ProviderDeepSeek || !strings.Contains(model, "claude-opus") {
		t.Errorf("sonnet 应映射到 deepseek/claude-opus-*，实际=%s/%s", p, model)
	}
	if p, model, _ := m.Map("haiku"); p != llm.ProviderDashScope || model != expectedHaikuModel {
		t.Errorf("haiku 应映射到 dashscope/%s, 实际=%s/%s", expectedHaikuModel, p, model)
	}
}

func TestModelMapping_Map(t *testing.T) {
	m := DefaultModelMapping()
	tests := []struct {
		vendor  string
		wantErr bool
	}{
		{"opus", false},
		{"sonnet", false},
		{"haiku", false},
		{"unknown", true},
		{"", true},
	}
	for _, tt := range tests {
		_, _, err := m.Map(tt.vendor)
		if tt.wantErr && err == nil {
			t.Errorf("vendor=%q 应报错", tt.vendor)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("vendor=%q 应通过，实际=%v", tt.vendor, err)
		}
	}
}

func TestModelMapping_Set(t *testing.T) {
	m := DefaultModelMapping()
	// 用户覆盖 haiku 到 deepseek
	err := m.Set("haiku", MappingEntry{
		Provider: llm.ProviderDeepSeek,
		Model:    "claude-haiku-3-5-20241022",
	})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	p, model, _ := m.Map("haiku")
	if p != llm.ProviderDeepSeek {
		t.Errorf("覆盖后 Provider=%s, want deepseek", p)
	}
	if model != "claude-haiku-3-5-20241022" {
		t.Errorf("覆盖后 Model=%s", model)
	}
}

func TestModelMapping_SetInvalidVendor(t *testing.T) {
	m := DefaultModelMapping()
	err := m.Set("gpt-5", MappingEntry{Provider: "openai", Model: "gpt-5"})
	if err == nil {
		t.Error("Set 非法 vendor model 应报错")
	}
}

func TestModelMapping_NilSafety(t *testing.T) {
	var m *ModelMapping
	_, _, err := m.Map("opus")
	if err == nil {
		t.Error("nil mapping 应报错")
	}
}

func TestParseModelMapping_Default(t *testing.T) {
	m, err := ParseModelMapping(`
model_mapping:
  opus:
    provider: custom-opus
    model: "custom-opus-model"
`)
	if err != nil {
		t.Fatalf("ParseModelMapping: %v", err)
	}
	// opus 应被覆盖
	p, model, _ := m.Map("opus")
	if p != "custom-opus" {
		t.Errorf("opus provider 应被覆盖为 custom-opus，实际=%s", p)
	}
	if model != "custom-opus-model" {
		t.Errorf("opus model 应被覆盖，实际=%s", model)
	}
	// sonnet/haiku 应保持默认值
	if _, _, err := m.Map("sonnet"); err != nil {
		t.Errorf("sonnet 应保持默认，实际=%v", err)
	}
}

func TestLoadModelMapping_NotExist(t *testing.T) {
	m, err := LoadModelMapping("D:/nonexistent/agent-models.yaml")
	if err != nil {
		t.Fatalf("LoadModelMapping 找不到文件应返回默认，不应报错：%v", err)
	}
	if m == nil {
		t.Fatal("找不到文件时返回 nil")
	}
	// 应等价于 DefaultModelMapping
	if _, _, err := m.Map("opus"); err != nil {
		t.Errorf("默认 opus 应映射成功：%v", err)
	}
}

func TestLoadModelMapping_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-models.yaml")
	content := `# 测试配置
model_mapping:
  opus:
    provider: test-opus
    model: "test-opus-model"
  sonnet:
    provider: test-sonnet
    model: "test-sonnet-model"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m, err := LoadModelMapping(path)
	if err != nil {
		t.Fatalf("LoadModelMapping: %v", err)
	}
	p, model, _ := m.Map("opus")
	if p != "test-opus" || model != "test-opus-model" {
		t.Errorf("opus 加载错：%s/%s", p, model)
	}
	// haiku 应保持默认（文件里没写）
	dp, dm, _ := m.Map("haiku")
	if dp != llm.ProviderDashScope || dm != expectedHaikuModel {
		t.Errorf("haiku 应保持默认，实际=%s/%s", dp, dm)
	}
}

// --- A1.9 prompt_adapter tests ---

func TestAdaptPrompt_ReplacesOpus(t *testing.T) {
	p := "You are opus. Use opus to write stories."
	out := AdaptPrompt(p)
	if strings.Contains(out, "opus") {
		t.Errorf("opus 应被替换，实际输出=%q", out)
	}
	if !strings.Contains(out, "高质量写作模型") {
		t.Errorf("应含 '高质量写作模型'，实际=%q", out)
	}
}

func TestAdaptPrompt_ReplacesSonnet(t *testing.T) {
	p := "Use sonnet for reasoning tasks."
	out := AdaptPrompt(p)
	if strings.Contains(out, "sonnet") {
		t.Errorf("sonnet 应被替换")
	}
	if !strings.Contains(out, "中档推理模型") {
		t.Errorf("应含 '中档推理模型'，实际=%q", out)
	}
}

func TestAdaptPrompt_ReplacesHaiku(t *testing.T) {
	p := "haiku is for lightweight tasks."
	out := AdaptPrompt(p)
	if strings.Contains(out, "haiku") {
		t.Errorf("haiku 应被替换")
	}
	if !strings.Contains(out, "轻量级模型") {
		t.Errorf("应含 '轻量级模型'，实际=%q", out)
	}
}

func TestAdaptPrompt_SimplifiesXMLTags(t *testing.T) {
	p := "Some intro.\n<thinking>Think about X</thinking>\nThen do Y."
	out := AdaptPrompt(p)
	if strings.Contains(out, "<thinking>") || strings.Contains(out, "</thinking>") {
		t.Errorf("XML 标签应被简化，实际=%q", out)
	}
	if !strings.Contains(out, "思考：") {
		t.Errorf("应含 '思考：'，实际=%q", out)
	}
	if !strings.Contains(out, "Think about X") {
		t.Errorf("应保留标签内文字，实际=%q", out)
	}
}

func TestAdaptPrompt_SimplifiesOutputTag(t *testing.T) {
	p := "Do this.\n<output>Result A</output>"
	out := AdaptPrompt(p)
	if !strings.Contains(out, "输出：") {
		t.Errorf("应含 '输出：'，实际=%q", out)
	}
}

func TestAdaptPrompt_EnglishDirectiveToChinese(t *testing.T) {
	p := "You are a story writer.\nYou must follow the rules."
	out := AdaptPrompt(p)
	if strings.Contains(out, "You are ") {
		t.Errorf("'You are' 应被改写")
	}
	if !strings.Contains(out, "你") {
		t.Errorf("应含 '你'，实际=%q", out)
	}
}

func TestAdaptPrompt_CollapsesBlankLines(t *testing.T) {
	p := "Line 1\n\n\n\n\nLine 2"
	out := AdaptPrompt(p)
	if strings.Contains(out, "\n\n\n") {
		t.Errorf("3+ 空行应被压缩，实际=%q", out)
	}
}

func TestAdaptPrompt_Idempotent(t *testing.T) {
	p := "You are a writer.\nUse opus for writing."
	out1 := AdaptPrompt(p)
	out2 := AdaptPrompt(out1)
	if out1 != out2 {
		t.Errorf("AdaptPrompt 应幂等（两次结果相同）\nout1=%q\nout2=%q", out1, out2)
	}
}

func TestNeedsAdaptation(t *testing.T) {
	tests := []struct {
		prompt string
		want   bool
	}{
		{"Plain Chinese text", false},
		{"You are a writer", true},       // 英文指令
		{"Use opus for writing", true},   // vendor 引用
		{"<thinking>X</thinking>", true}, // XML 标签
		{"normal prompt", false},
	}
	for i, tt := range tests {
		got := NeedsAdaptation(tt.prompt)
		if got != tt.want {
			t.Errorf("case %d: NeedsAdaptation(%q)=%v, want %v", i, tt.prompt, got, tt.want)
		}
	}
}

func TestAdaptPrompt_PreservesContent(t *testing.T) {
	// 不应误伤正文内容
	p := "Use opus for writing chapters."
	out := AdaptPrompt(p)
	if !strings.Contains(out, "writing chapters") {
		t.Errorf("应保留正文 'writing chapters'，实际=%q", out)
	}
}
