package tools

import (
	"fmt"
	"strings"
	"testing"
)

// TestWebSearchTool_Basic 测试 WebSearch 基本调用
func TestWebSearchTool_Basic(t *testing.T) {
	t.Run("valid query", func(t *testing.T) {
		tool := NewWebSearchTool()
		res, _ := tool.Execute(nil, []byte(`{"query":"novel writing techniques","max_results":3}`))
		if res.IsError {
			t.Errorf("valid query 应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "novel writing techniques") {
			t.Errorf("结果应含 query 字符串")
		}
		if !strings.Contains(res.Content, "MOCK") {
			t.Errorf("结果应标注 MOCK")
		}
	})
	t.Run("empty query", func(t *testing.T) {
		tool := NewWebSearchTool()
		res, _ := tool.Execute(nil, []byte(`{"query":""}`))
		if !res.IsError {
			t.Errorf("空 query 应报错")
		}
	})
}

// TestAgentBrowserTool_Basic 测试 AgentBrowser
func TestAgentBrowserTool_Basic(t *testing.T) {
	t.Run("with actions", func(t *testing.T) {
		tool := NewAgentBrowserTool()
		res, _ := tool.Execute(nil, []byte(`{"url":"https://example.com","actions":["click .btn","fill #input 'text'","scroll down"]}`))
		if res.IsError {
			t.Errorf("应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "https://example.com") {
			t.Errorf("结果应含 URL")
		}
		if !strings.Contains(res.Content, "click .btn") {
			t.Errorf("结果应含 actions")
		}
	})
	t.Run("no actions", func(t *testing.T) {
		tool := NewAgentBrowserTool()
		res, _ := tool.Execute(nil, []byte(`{"url":"https://example.com"}`))
		if res.IsError {
			t.Errorf("无 actions 也应成功（mock）")
		}
	})
}

// TestCDPTool_Basic 测试 CDP 工具
func TestCDPTool_Basic(t *testing.T) {
	tests := []struct {
		cmd    string
		params string
		expect string
	}{
		{"Page.navigate", `{"url":"https://example.com"}`, "Navigated"},
		{"Runtime.evaluate", `{"expression":"document.title"}`, "Expression"},
		{"Page.captureScreenshot", `{}`, "Screenshot"},
		{"Unknown.command", `{}`, "Command executed"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			c := NewCDPTool()
			res, _ := c.Execute(nil, []byte(fmt.Sprintf(`{"command":"%s","params":%s}`, tt.cmd, tt.params)))
			if res.IsError {
				t.Errorf("应成功：%v", res.Content)
			}
			if !strings.Contains(res.Content, tt.expect) {
				t.Errorf("结果应含 '%s'，实际=%q", tt.expect, res.Content)
			}
		})
	}
}

// TestNewTools_Registration 测试 3 个新 tool 可以注册到 Registry
func TestNewTools_Registration(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(NewWebSearchTool()); err != nil {
		t.Errorf("Register WebSearch: %v", err)
	}
	if err := reg.Register(NewAgentBrowserTool()); err != nil {
		t.Errorf("Register AgentBrowser: %v", err)
	}
	if err := reg.Register(NewCDPTool()); err != nil {
		t.Errorf("Register CDP: %v", err)
	}
	if reg.Count() != 3 {
		t.Errorf("应有 3 个 tool，实际=%d", reg.Count())
	}
	// Verify names
	for _, name := range []string{"WebSearch", "AgentBrowser", "CDP"} {
		if !reg.Has(name) {
			t.Errorf("Has(%q) 应为 true", name)
		}
	}
}
