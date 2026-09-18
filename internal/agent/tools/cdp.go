package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CDPTool Chrome DevTools Protocol 工具 (Sprint A5.12)
//
// 当前为 MOCK 实现：返回预置的 CDP 命令结果。
//
// 真实实现路线：
//   - A6 接 chromedp 库（github.com/chromedp/chromedp）
//   - 或接 cdp 库直接驱动 headless Chrome
type CDPTool struct{}

// NewCDPTool 构造
func NewCDPTool() *CDPTool { return &CDPTool{} }

// Name 工具名
func (t *CDPTool) Name() string { return "CDP" }

// Description 工具描述
func (t *CDPTool) Description() string {
	return "Chrome DevTools Protocol 命令执行（click/fill/eval/screenshot）。当前 MOCK 实现。"
}

// InputSchema JSON Schema
func (t *CDPTool) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "CDP 命令名（如 'Page.navigate'）"},
    "params": {"type": "object", "description": "命令参数"}
  },
  "required": ["command"]
}`)
}

// Execute mock 实现
func (t *CDPTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in struct {
		Command string         `json:"command"`
		Params  map[string]any `json:"params"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("CDP: invalid input: %v", err)), nil
	}
	if strings.TrimSpace(in.Command) == "" {
		return ErrorResult("CDP: command is required"), nil
	}

	// Mock 返回
	content := fmt.Sprintf("CDP %s result (mock):\n", in.Command)
	switch in.Command {
	case "Page.navigate":
		content += fmt.Sprintf("  Navigated to: %v\n", in.Params["url"])
		content += "  Status: 200\n"
	case "Runtime.evaluate":
		content += fmt.Sprintf("  Expression: %v\n", in.Params["expression"])
		content += "  Result: <mock value>\n"
	case "Page.captureScreenshot":
		content += "  Screenshot saved: /tmp/mock-screenshot.png (64KB base64)\n"
	default:
		content += fmt.Sprintf("  Command executed: %s\n", in.Command)
	}
	content += "\nNOTE: MOCK data. Sprint A6 will integrate chromedp library."
	return SuccessResult(content), nil
}
