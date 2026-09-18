package tools

// A6.12 mock tools 集成 e2e (Sprint A6.12)
//
// 验证 3 个 mock tool（WebSearch / AgentBrowser / CDP）
// 跟 ToolAdapter 的完整集成：
//   1. ToolAdapter.LLMTools 注册 3 个 mock tool 后能正确转 llm.Tool
//   2. ToolAdapter.Dispatch 调 mock tool 能正常返回（非 error）
//   3. 输入参数正确传递（query / url / command）
//
// 注意：本测试需要 internal/agent 包内的 ToolAdapter，所以 import agent 包会引入循环依赖。
// 因此本测试只测 tools.Registry 路径，ToolAdapter 集成测试见 internal/agent 包内的相关测试。

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMockToolsE2E_LLMToolsAllExposed - 3 个 mock tool 注册 + Schemas 完整
//
// 验证：注册 WebSearch + AgentBrowser + CDP 后
//   - reg.Count() == 3
//   - reg.Has() 返回 true
//   - reg.Schemas() 返回 3 个完整 schema（Name/Description/InputSchema 都非空）
//   - 每个 tool 的 InputSchema 都是合法 JSON
//nolint:gocyclo // 3 mock tool 注册 + 6 schema 字段断言，结构清晰不需拆分
func TestMockToolsE2E_LLMToolsAllExposed(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(NewWebSearchTool()); err != nil {
		t.Fatalf("Register WebSearch: %v", err)
	}
	if err := reg.Register(NewAgentBrowserTool()); err != nil {
		t.Fatalf("Register AgentBrowser: %v", err)
	}
	if err := reg.Register(NewCDPTool()); err != nil {
		t.Fatalf("Register CDP: %v", err)
	}

	if reg.Count() != 3 {
		t.Errorf("reg.Count 应=3，实际=%d", reg.Count())
	}

	// Has 检查
	for _, name := range []string{"WebSearch", "AgentBrowser", "CDP"} {
		if !reg.Has(name) {
			t.Errorf("reg.Has(%q) 应为 true", name)
		}
	}

	// Schemas 完整
	schemas := reg.Schemas()
	if len(schemas) != 3 {
		t.Errorf("reg.Schemas() 应返回 3 个，实际=%d", len(schemas))
	}
	for _, s := range schemas {
		if s.Name == "" {
			t.Errorf("schema.Name 应非空")
		}
		if s.Description == "" {
			t.Errorf("schema %q: Description 应非空", s.Name)
		}
		if len(s.InputSchema) == 0 {
			t.Errorf("schema %q: InputSchema 应非空", s.Name)
		}
		// 验证 InputSchema 是合法 JSON
		var js map[string]any
		if err := json.Unmarshal(s.InputSchema, &js); err != nil {
			t.Errorf("schema %q: InputSchema 不是合法 JSON：%v", s.Name, err)
		}
		// 验证含 "type":"object"
		if jsType, ok := js["type"].(string); !ok || jsType != "object" {
			t.Errorf("schema %q: 应为 object type，实际=%v", s.Name, js["type"])
		}
	}

	// List 按字典序
	names := reg.List()
	if len(names) != 3 {
		t.Errorf("List 应返 3 个，实际=%d", len(names))
	}
	wantSorted := []string{"AgentBrowser", "CDP", "WebSearch"} // 字典序
	for i, w := range wantSorted {
		if names[i] != w {
			t.Errorf("List[%d]=%q, want %q", i, names[i], w)
		}
	}

	t.Logf("✅ 3 个 mock tool 注册 + schema 验证通过")
}

// TestMockToolsE2E_DispatchEachTool - 3 个 mock tool 单独调 Dispatch
//
// 验证：每个 mock tool 调 Dispatch(name, args) 都返回成功 result（IsError=false），
// 且 result.Content 含关键字（query/url/command 回显）。
func TestMockToolsE2E_DispatchEachTool(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(NewWebSearchTool())
	_ = reg.Register(NewAgentBrowserTool())
	_ = reg.Register(NewCDPTool())

	execCtx := &ExecContext{Root: "/tmp"}

	tests := []struct {
		name           string
		toolName       string
		input          string
		expectContains string
	}{
		{
			name:           "WebSearch_basic",
			toolName:       "WebSearch",
			input:          `{"query": "Go testing", "max_results": 2}`,
			expectContains: "Go testing",
		},
		{
			name:           "AgentBrowser_basic",
			toolName:       "AgentBrowser",
			input:          `{"url": "https://example.com"}`,
			expectContains: "https://example.com",
		},
		{
			name:           "CDP_navigate",
			toolName:       "CDP",
			input:          `{"command": "Page.navigate", "params": {"url": "https://example.com"}}`,
			expectContains: "Navigated",
		},
		{
			name:           "CDP_screenshot",
			toolName:       "CDP",
			input:          `{"command": "Page.captureScreenshot"}`,
			expectContains: "Screenshot",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			res, _ := reg.Dispatch(tt.toolName, execCtx, []byte(tt.input))
			if res.IsError {
				t.Errorf("Dispatch(%q) 应成功，实际错误=%q", tt.toolName, res.Content)
			}
			if !strings.Contains(res.Content, tt.expectContains) {
				t.Errorf("Dispatch(%q) result 应含 %q，实际=%q", tt.toolName, tt.expectContains, truncateStr(res.Content, 100))
			}
			// mock tool 应标 MOCK（提醒用户这是 mock 数据）
			if !strings.Contains(res.Content, "MOCK") {
				t.Errorf("Dispatch(%q) result 应标 'MOCK'，实际前 100 字符=%q", tt.toolName, truncateStr(res.Content, 100))
			}
		})
	}
}

// TestMockToolsE2E_AdapterIntegration - adapter 集成测试 (Sprint A6.12)
//
// 注：ToolAdapter 在 internal/agent 包，本测试在 internal/agent/tools 包。
// 这里只测 tools 层（registry + Dispatch），
// agent 包的 ToolAdapter 集成测试在 internal/agent/dispatcher_orchestrator_test.go 已覆盖。
func TestMockToolsE2E_AdapterIntegration(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(NewWebSearchTool())
	_ = reg.Register(NewAgentBrowserTool())
	_ = reg.Register(NewCDPTool())

	execCtx := &ExecContext{Root: "/tmp"}

	// 验证：Dispatch 通过 registry 路径调 3 个 tool 都成功
	toolNames := []string{"WebSearch", "AgentBrowser", "CDP"}
	for _, name := range toolNames {
		t.Run(name, func(t *testing.T) {
			args := []byte(`{}`)
			switch name {
			case "WebSearch":
				args = []byte(`{"query": "test"}`)
			case "AgentBrowser":
				args = []byte(`{"url": "https://example.com"}`)
			case "CDP":
				args = []byte(`{"command": "Page.navigate"}`)
			}
			res, _ := reg.Dispatch(name, execCtx, args)
			if res.IsError {
				t.Errorf("Dispatch(%q) 应成功，实际错误=%q", name, res.Content)
			}
		})
	}
}

// truncateStr 截断字符串到指定 rune 数（用于 error message 输出，避免切碎 UTF-8）
func truncateStr(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}