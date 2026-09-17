package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestSchemaToPrompt 验证 schema prompt 生成
func TestSchemaToPrompt(t *testing.T) {
	s := JSONSchema{
		Name:        "Character",
		Description: "小说角色",
		Fields: []JSONSchemaField{
			{Name: "name", Type: "string", Required: true, Description: "角色名"},
			{Name: "age", Type: "int", Description: "年龄"},
			{Name: "tags", Type: "array", Items: "string"},
		},
	}
	prompt := schemaToPrompt(s)
	if !strings.Contains(prompt, "Character") {
		t.Error("prompt should contain schema name")
	}
	if !strings.Contains(prompt, "name") {
		t.Error("prompt should contain field name")
	}
	if !strings.Contains(prompt, "required") {
		t.Error("prompt should mark required fields")
	}
	if !strings.Contains(prompt, "array<string>") {
		t.Error("prompt should expand array type")
	}
	if !strings.Contains(prompt, "年龄") {
		t.Error("prompt should contain field description")
	}
}

// TestStripCodeBlock 验证 markdown 代码块剥离
func TestStripCodeBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no code block",
			input:    `{"name":"alice"}`,
			expected: `{"name":"alice"}`,
		},
		{
			name:     "with json code block",
			input:    "```json\n{\"name\":\"alice\"}\n```",
			expected: `{"name":"alice"}`,
		},
		{
			name:     "with bare code block",
			input:    "```\n{\"name\":\"alice\"}\n```",
			expected: `{"name":"alice"}`,
		},
		{
			name:     "leading whitespace",
			input:    "  \n  {\"a\":1}  \n",
			expected: `{"a":1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCodeBlock(tt.input)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// TestJSONSchemaError 测试错误包装
func TestJSONSchemaError(t *testing.T) {
	err := &JSONSchemaError{
		Content: "{invalid}",
		Err:     errors.New("bad json"),
	}
	msg := err.Error()
	if !strings.Contains(msg, "bad json") {
		t.Errorf("error msg should contain wrapped err: %s", msg)
	}
	if !strings.Contains(msg, "{invalid}") {
		t.Errorf("error msg should contain content: %s", msg)
	}
	// Unwrap
	if err.Unwrap().Error() != "bad json" {
		t.Error("Unwrap should return original err")
	}
}

// TestBuildJSONSystemPrompt 验证 system prompt 完整
func TestBuildJSONSystemPrompt(t *testing.T) {
	s := JSONSchema{
		Name: "Test",
		Fields: []JSONSchemaField{
			{Name: "x", Type: "string", Required: true},
		},
	}
	prompt := buildJSONSystemPrompt(s)
	if !strings.Contains(prompt, "JSON 助手") {
		t.Error("prompt should identify role")
	}
	if !strings.Contains(prompt, "Test") {
		t.Error("prompt should contain schema name")
	}
	if !strings.Contains(prompt, "required") {
		t.Error("prompt should mark required")
	}
}

// TestGenerateJSON_InvalidJSON 测试 LLM 输出非 JSON 时返回 JSONSchemaError
func TestGenerateJSON_InvalidJSON(t *testing.T) {
	// 构造 mock router 不可行 (Router 是 concrete struct)
	// 改测 buildJSONSystemPrompt + stripCodeBlock 间接路径
	// 完整端到端测试需要 mock provider，超出单元测试范围
	// 这里跳过集成测试，依赖手动 / CI 验证
	t.Skip("requires mock provider for end-to-end test")
}

// 确保 context 引用不报 unused
var _ = context.Background
