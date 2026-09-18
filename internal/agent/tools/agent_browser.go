package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AgentBrowserTool 浏览器自动化工具 (Sprint A5.11)
//
// 当前为 MOCK 实现：返回预置 DOM/HTML 结构。
//
// 真实实现路线：
//   - A6 接 MCP `agent-browser` 服务（默认 localhost:8000）
//   - 或接 chromedp 库驱动 headless Chrome
type AgentBrowserTool struct{}

// NewAgentBrowserTool 构造
func NewAgentBrowserTool() *AgentBrowserTool { return &AgentBrowserTool{} }

// Name 工具名
func (t *AgentBrowserTool) Name() string { return "AgentBrowser" }

// Description 工具描述
func (t *AgentBrowserTool) Description() string {
	return "驱动 headless browser 访问页面。接受 URL + actions（click/fill/scroll）。当前 MOCK 实现。"
}

// InputSchema JSON Schema
func (t *AgentBrowserTool) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "目标 URL"},
    "actions": {"type": "array", "description": "操作序列", "items": {"type": "string"}}
  },
  "required": ["url"]
}`)
}

// Execute mock 实现
func (t *AgentBrowserTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in struct {
		URL     string   `json:"url"`
		Actions []string `json:"actions"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("AgentBrowser: invalid input: %v", err)), nil
	}
	if strings.TrimSpace(in.URL) == "" {
		return ErrorResult("AgentBrowser: url is required"), nil
	}

	// Mock 返回 HTML + actions 序列
	content := fmt.Sprintf("Browser session:\n")
	content += "URL: " + in.URL + "\n"
	content += "HTML content (mock):\n"
	content += "  <html><body><h1>Mock Page</h1><p>This is MOCK content.</p></body></html>\n"
	if len(in.Actions) > 0 {
		content += "\nActions executed:\n"
		for i, a := range in.Actions {
			content += fmt.Sprintf("  %d. %s ✓\n", i+1, a)
		}
	}
	content += "\nNOTE: MOCK data. Sprint A6 will integrate MCP agent-browser or chromedp."
	return SuccessResult(content), nil
}
