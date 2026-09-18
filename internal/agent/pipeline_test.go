package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/agent/tools"
	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// mockLLMProvider mock LLM provider (Sprint A5.6 端到端测试用)
//
// 不调真实 API，返回预置 response。
type mockLLMProvider struct {
	// Responses 按调用顺序返回的 responses
	Responses []*llm.ChatWithToolsResponse
	// Errors 按调用顺序返回的 errors
	Errors []error
	// Calls 实际收到的请求（验证参数用）
	Calls []llm.ChatWithToolsRequest
	// callIndex 下一个要返回的 response
	callIndex int
}

func (m *mockLLMProvider) ChatWithTools(ctx context.Context, req llm.ChatWithToolsRequest) (*llm.ChatWithToolsResponse, error) {
	m.Calls = append(m.Calls, req)
	if m.callIndex >= len(m.Responses) {
		return nil, errors.New("mockLLM: no more responses")
	}
	if m.callIndex < len(m.Errors) && m.Errors[m.callIndex] != nil {
		err := m.Errors[m.callIndex]
		m.callIndex++
		return nil, err
	}
	resp := m.Responses[m.callIndex]
	m.callIndex++
	return resp, nil
}

// mockTool mock tool 用于测试
type mockTool struct {
	name        string
	description string
	schema      []byte
	execFunc    func(input []byte) tools.Result
	calls       int
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.description }
func (m *mockTool) InputSchema() []byte { return m.schema }
func (m *mockTool) Execute(ctx *tools.ExecContext, input []byte) (tools.Result, error) {
	m.calls++
	if m.execFunc != nil {
		return m.execFunc(input), nil
	}
	return tools.SuccessResult("mock-ok"), nil
}

// TestRunAgent_NoToolCalls 测试 LLM 直接返回 final answer（无 tool calls）
func TestRunAgent_NoToolCalls(t *testing.T) {
	spec := &AgentSpec{
		Name:         "test-agent",
		Model:        "sonnet",
		MaxTurns:     5,
		SystemPrompt: "You are a test agent.",
	}

	mockLLM := &mockLLMProvider{
		Responses: []*llm.ChatWithToolsResponse{
			{
				Content:   "Hello from mock LLM",
				TokensIn:  10,
				TokensOut: 20,
			},
		},
	}

	toolsReg := tools.NewRegistry()
	cfg := LoopConfig{
		Tools:       NewToolAdapter(toolsReg, "/tmp"),
		ProjectRoot: "/tmp",
		Mapping:     DefaultModelMapping(),
	}
	cfg.Router = mockLLM // mockLLMProvider implements LLMChat

	res, err := RunAgent(context.Background(), spec, cfg, "ping")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.Content != "Hello from mock LLM" {
		t.Errorf("Content=%q", res.Content)
	}
	if res.TokensIn != 10 || res.TokensOut != 20 {
		t.Errorf("Tokens in/out 错：%d/%d", res.TokensIn, res.TokensOut)
	}
	if len(res.ToolCalls) != 0 {
		t.Errorf("无 tool_calls 但 ToolCalls=%v", res.ToolCalls)
	}
}

// mockLLMProvider now satisfies LLMChat directly (no mockRouter wrapper needed)

// TestRunAgent_WithToolCalls 测试 LLM 调 1 次 tool 然后返回 final
func TestRunAgent_WithToolCalls(t *testing.T) {
	spec := &AgentSpec{
		Name:     "test-agent",
		Model:    "sonnet",
		MaxTurns: 5,
	}

	// LLM 第一轮：调 Read，第二轮：返回 final answer
	mockLLM := &mockLLMProvider{
		Responses: []*llm.ChatWithToolsResponse{
			{
				Content: "",
				ToolCalls: []llm.ExecutedToolCall{
					{
						Call: llm.ToolCall{
							ID:        "call_1",
							Name:      "MockRead",
							Arguments: json.RawMessage(`{"path": "/tmp/test.md"}`),
						},
						Result: llm.ToolResult{
							CallID: "call_1",
							Result: "file content here",
						},
					},
				},
				TokensIn: 5,
			},
			{
				Content:   "I read the file and here's the answer",
				TokensIn:  8,
				TokensOut: 15,
			},
		},
	}

	mockT := &mockTool{
		name:        "MockRead",
		description: "Mock read tool",
		schema:      []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		execFunc: func(input []byte) tools.Result {
			return tools.SuccessResult("file content here")
		},
	}

	toolsReg := tools.NewRegistry()
	_ = toolsReg.Register(mockT)

	cfg := LoopConfig{
		Router:      mockLLM,
		Tools:       NewToolAdapter(toolsReg, "/tmp"),
		ProjectRoot: "/tmp",
		Mapping:     DefaultModelMapping(),
	}

	res, err := RunAgent(context.Background(), spec, cfg, "read /tmp/test.md and summarize")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.Content != "I read the file and here's the answer" {
		t.Errorf("Content=%q", res.Content)
	}
	if len(res.ToolCalls) != 1 {
		t.Errorf("应有 1 个 ToolCall，实际=%d", len(res.ToolCalls))
	} else {
		tc := res.ToolCalls[0]
		if tc.Name != "MockRead" {
			t.Errorf("ToolCall.Name=%q", tc.Name)
		}
		if tc.Output != "file content here" {
			t.Errorf("ToolCall.Output=%q", tc.Output)
		}
	}
	if mockT.calls != 1 {
		t.Errorf("MockRead 应被调 1 次，实际=%d", mockT.calls)
	}
	if len(mockLLM.Calls) != 2 {
		t.Errorf("LLM 应被调 2 次，实际=%d", len(mockLLM.Calls))
	}
	// 第二轮 messages 应含 tool result
	if len(mockLLM.Calls) > 1 {
		msgs := mockLLM.Calls[1].Messages
		hasToolResult := false
		for _, m := range msgs {
			if strings.Contains(m.Content, "file content here") {
				hasToolResult = true
				break
			}
		}
		if !hasToolResult {
			t.Error("第二轮 messages 应含 tool result")
		}
	}
}

// TestRunAgent_MaxTurnsExceed 测试超过 maxTurns 时报错
func TestRunAgent_MaxTurnsExceed(t *testing.T) {
	spec := &AgentSpec{
		Name:     "loop-agent",
		Model:    "haiku",
		MaxTurns: 2,
	}

	// LLM 总调 tool（不返回 final answer）
	mockLLM := &mockLLMProvider{
		Responses: []*llm.ChatWithToolsResponse{
			{ToolCalls: []llm.ExecutedToolCall{{Call: llm.ToolCall{ID: "c1", Name: "Mock", Arguments: []byte(`{}`)}}}, TokensIn: 1},
			{ToolCalls: []llm.ExecutedToolCall{{Call: llm.ToolCall{ID: "c2", Name: "Mock", Arguments: []byte(`{}`)}}}, TokensIn: 1},
			{ToolCalls: []llm.ExecutedToolCall{{Call: llm.ToolCall{ID: "c3", Name: "Mock", Arguments: []byte(`{}`)}}}, TokensIn: 1},
		},
	}
	mockT := &mockTool{name: "Mock", description: "m", schema: []byte(`{"type":"object"}`)}
	toolsReg := tools.NewRegistry()
	_ = toolsReg.Register(mockT)

	cfg := LoopConfig{
		Router:      mockLLM,
		Tools:       NewToolAdapter(toolsReg, "/tmp"),
		ProjectRoot: "/tmp",
		Mapping:     DefaultModelMapping(),
	}

	_, err := RunAgent(context.Background(), spec, cfg, "loop forever")
	if err == nil {
		t.Fatal("应报 max turns exceeded")
	}
	if !strings.Contains(err.Error(), "max turns") {
		t.Errorf("错误信息应含 'max turns'，实际=%q", err.Error())
	}
}

// TestRunAgent_ContextCanceled 测试 ctx 取消
func TestRunAgent_ContextCanceled(t *testing.T) {
	spec := &AgentSpec{Name: "ctx-agent", Model: "haiku", MaxTurns: 5}
	mockLLM := &mockLLMProvider{} // 没有 response → 会 panic
	mockT := &mockTool{name: "M", description: "m", schema: []byte(`{"type":"object"}`)}
	toolsReg := tools.NewRegistry()
	_ = toolsReg.Register(mockT)

	cfg := LoopConfig{
		Router:      mockLLM,
		Tools:       NewToolAdapter(toolsReg, "/tmp"),
		ProjectRoot: "/tmp",
		Mapping:     DefaultModelMapping(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := RunAgent(ctx, spec, cfg, "test")
	if err == nil {
		t.Fatal("ctx canceled 应报错")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("错误信息应含 'canceled'，实际=%q", err.Error())
	}
}

// TestPathTranslator 测试 vendor .claude/skills/ → Go internal/skills/ 翻译
func TestPathTranslator(t *testing.T) {
	pt := NewPathTranslator()

	tests := []struct {
		in   string
		want string
	}{
		{
			"读取 vendor skills/story-setup/SKILL.md",
			"读取 internal/skills/assets/story-setup/SKILL.md",
		},
		{
			"读取 {项目根}/skills/story-setup/references/diagnostics.md",
			"读取 internal/skills/assets/story-setup/references/diagnostics.md",
		},
		{
			"vendor skills/story-long-write/references/genre-prose-cards/foo.md",
			"internal/skills/assets/story-long-write/references/genre-prose-cards/foo.md",
		},
		{
			".claude/skills/story-setup/references/diagnostics.md",
			"internal/skills/assets/story-setup/references/diagnostics.md",
		},
		{
			"~/.claude/skills/story-setup/SKILL.md",
			"internal/skills/assets/story-setup/SKILL.md",
		},
		{
			"没有 vendor 路径的普通文本",
			"没有 vendor 路径的普通文本",
		},
	}
	for _, tt := range tests {
		got := pt.Translate(tt.in)
		if got != tt.want {
			t.Errorf("Translate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestBuildLLMTools_DisallowedTools 测试 vendor DisallowedTools enforce
func TestBuildLLMTools_DisallowedTools(t *testing.T) {
	toolsReg := tools.NewRegistry()
	_ = toolsReg.Register(&mockTool{name: "Read", description: "r", schema: []byte(`{}`)})
	_ = toolsReg.Register(&mockTool{name: "Write", description: "w", schema: []byte(`{}`)})
	_ = toolsReg.Register(&mockTool{name: "Edit", description: "e", schema: []byte(`{}`)})

	adapter := NewToolAdapter(toolsReg, "/tmp")

	// 模拟只读 role：vendor spec 允许 [Read]，disallowed [Write, Edit]
	llmTools := buildLLMTools(
		[]string{"Read", "Write", "Edit"},
		adapter,
		[]string{"Write", "Edit"}, // vendor disallowedTools
	)
	if len(llmTools) != 1 {
		t.Errorf("expected 1 tool after enforce, got %d", len(llmTools))
	}
	if len(llmTools) > 0 && llmTools[0].Name != "Read" {
		t.Errorf("expected Read, got %q", llmTools[0].Name)
	}
}
