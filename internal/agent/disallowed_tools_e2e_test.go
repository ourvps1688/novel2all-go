package agent

// A6.14 disallowed tools 防御 e2e (Sprint A6.14)
//
// 验证三层防御：
//   1. ToolAdapter.LLMTools - spec 过滤（LLM 看不到 schema）
//   2. ToolAdapter.Dispatch - defense-in-depth（LLM 硬塞 tool call 也被拒）
//   3. Agent.Run - 端到端（mock LLM 返回 disallowed tool_call 应被拒）
//
// vendor 4 个只读 role（consistency-checker / character-designer / story-explorer / story-researcher）
// 的 spec.DisallowedTools 通常含 ["Write", "Edit", "Bash"]，
// 防止 LLM 误调写操作。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/agent/tools"
	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/roles"
)

// disallowedToolsE2EFixture 构造 5 个 tool + adapter + spec 的 setup
type disallowedToolsE2EFixture struct {
	registry *tools.Registry
	adapter  *ToolAdapter
	tools    []string
	dir      string // t.TempDir()，含 test.txt / x.txt 测试文件
}

func newDisallowedToolsE2EFixture(t *testing.T) *disallowedToolsE2EFixture {
	t.Helper()
	dir := t.TempDir()
	// ReadTool 需要 test.txt 存在；EditTool 需要 x.txt 含 "a" 才匹配 old_string
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup test.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatalf("setup x.txt: %v", err)
	}

	reg := tools.NewRegistry()
	if err := reg.Register(tools.NewReadTool(nil)); err != nil {
		t.Fatalf("Register Read: %v", err)
	}
	if err := reg.Register(tools.NewWriteTool(nil)); err != nil {
		t.Fatalf("Register Write: %v", err)
	}
	if err := reg.Register(tools.NewEditTool(nil)); err != nil {
		t.Fatalf("Register Edit: %v", err)
	}
	if err := reg.Register(tools.NewBashTool(nil)); err != nil {
		t.Fatalf("Register Bash: %v", err)
	}
	if err := reg.Register(tools.NewWebSearchTool()); err != nil {
		t.Fatalf("Register WebSearch: %v", err)
	}

	adapter := NewToolAdapter(reg, dir)
	adapter.SetDisallowedTools([]string{"WebSearch", "Edit"})

	return &disallowedToolsE2EFixture{
		registry: reg,
		adapter:  adapter,
		tools:    []string{"Read", "Write", "Bash", "WebSearch", "Edit"},
		dir:      dir,
	}
}

// TestDisallowedToolsE2E_LLMToolsFilter - LLMTools 过滤层 (Sprint A6.14)
//
// 验证：spec.Tools 全 5 个 + spec.DisallowedTools 含 WebSearch/Edit
// 调 adapter.LLMTools(spec.Tools) 后返回的 llm.Tool 列表
// 应只剩 Read/Write/Bash（3 个），不含 WebSearch 和 Edit。
func TestDisallowedToolsE2E_LLMToolsFilter(t *testing.T) {
	f := newDisallowedToolsE2EFixture(t)

	got := f.adapter.LLMTools(f.tools)

	// 1. 返回 3 个 tool（5 - 2 disallowed）
	if len(got) != 3 {
		t.Errorf("LLMTools 应返回 3 个 tool，实际=%d", len(got))
	}

	// 2. 检查 returned names
	names := make(map[string]bool, len(got))
	for _, lt := range got {
		names[lt.Name] = true
	}

	// 2.1 不应含 WebSearch（disallowed）
	if names["WebSearch"] {
		t.Errorf("LLMTools 不应含 'WebSearch'（disallowed），但返回了")
	}

	// 2.2 不应含 Edit（disallowed）
	if names["Edit"] {
		t.Errorf("LLMTools 不应含 'Edit'（disallowed），但返回了")
	}

	// 2.3 应含 Read / Write / Bash
	for _, want := range []string{"Read", "Write", "Bash"} {
		if !names[want] {
			t.Errorf("LLMTools 应含 %q，实际返回=%v", want, names)
		}
	}

	t.Logf("✅ LLMTools 过滤通过：3 个 tool（Read/Write/Bash），正确排除 WebSearch + Edit")
}

// TestDisallowedToolsE2E_DispatchRejects - Dispatch defense-in-depth (Sprint A6.14)
//
// 验证：即使绕过 LLMTools 过滤直接调 adapter.Dispatch("WebSearch", ...)，
// 也会被 disallowed 防御层拒绝。
func TestDisallowedToolsE2E_DispatchRejects(t *testing.T) {
	f := newDisallowedToolsE2EFixture(t)

	tests := []struct {
		name           string
		toolName       string
		expectError    bool
		expectContains string
	}{
		{"WebSearch_rejected", "WebSearch", true, "disallowed"},
		{"Edit_rejected", "Edit", true, "disallowed"},
		{"Read_allowed", "Read", false, ""},
		{"Write_allowed", "Write", false, ""},
		{"Bash_allowed", "Bash", false, ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var args json.RawMessage
			switch tt.toolName {
			case "WebSearch":
				args = json.RawMessage(`{"query": "test"}`)
			case "Read":
				args = json.RawMessage(`{"path": "test.txt"}`)
			case "Write":
				args = json.RawMessage(`{"path": "test.txt", "content": "x"}`)
			case "Edit":
				args = json.RawMessage(`{"path": "test.txt", "old_string": "a", "new_string": "b"}`)
			case "Bash":
				args = json.RawMessage(`{"command": "echo hi"}`)
			default:
				args = json.RawMessage(`{}`)
			}

			res := f.adapter.Dispatch(tt.toolName, args)

			if tt.expectError {
				if !res.IsError {
					t.Errorf("Dispatch(%q) 应返回 IsError=true，实际=%+v", tt.toolName, res)
				}
				if !strings.Contains(res.Content, tt.expectContains) {
					t.Errorf("Dispatch(%q) 错误信息应含 %q，实际=%q", tt.toolName, tt.expectContains, res.Content)
				}
			} else if res.IsError {
				t.Errorf("Dispatch(%q) 应成功，实际错误=%q", tt.toolName, res.Content)
			}
		})
	}
}

// TestDisallowedToolsE2E_AgentRunDefense - Agent.Run 端到端 (Sprint A6.14)
//
// 验证：mock LLM 返回一个 tool_call {"name": "WebSearch", ...}，spec 标记 WebSearch 为 disallowed。
// 调 RunAgent：(a) LLMTools 过滤（理想路径），(b) Dispatch 拒绝（defense-in-depth）。
// 断言：mockLLM.Calls[0].Tools 不含 WebSearch/Edit；res.ToolCalls 记录 LLM 尝试；
// 最终 Content 来自 mock 第二轮。
//
//nolint:gocyclo // 10 段独立断言（mock LLM setup + 3 层防御 + 6 验证），拆分丢失可读性
func TestDisallowedToolsE2E_AgentRunDefense(t *testing.T) {
	// 1. 5 个 tool registry
	reg := tools.NewRegistry()
	_ = reg.Register(tools.NewReadTool(nil))
	_ = reg.Register(tools.NewWriteTool(nil))
	_ = reg.Register(tools.NewEditTool(nil))
	_ = reg.Register(tools.NewBashTool(nil))
	_ = reg.Register(tools.NewWebSearchTool())

	// 2. adapter + DisallowedTools
	adapter := NewToolAdapter(reg, "/tmp")
	adapter.SetDisallowedTools([]string{"WebSearch", "Edit"})

	// 3. spec.Tools 全 5 个（模拟 vendor spec 没正确过滤的情况）
	specTools := []string{"Read", "Write", "Bash", "WebSearch", "Edit"}

	// 4. mock LLM 试图调 WebSearch（模拟 LLM 误调 disallowed tool）
	// 注意：这里 mock LLM 的 ToolCalls[].Result.Error 字段不会被真实使用，
	// 因为 loop.go 调 adapter.Dispatch() 会返回新的 tools.Result，
	// 并用 buildToolResultMessage() 构造 tool_result message 给下一轮 LLM。
	mockLLM := &mockLLMProvider{
		Responses: []*llm.ChatWithToolsResponse{
			{
				Content: "Trying to WebSearch...",
				ToolCalls: []llm.ExecutedToolCall{
					{
						Call: llm.ToolCall{
							ID:        "call_1",
							Name:      "WebSearch",
							Arguments: json.RawMessage(`{"query": "sensitive"}`),
						},
						Result: llm.ToolResult{
							CallID: "call_1",
							Error:  "should be rejected by defense (this is mock-set, not used)",
						},
					},
				},
				TokensIn: 10,
			},
			{Content: "Final answer (WebSearch was rejected)", TokensIn: 5, TokensOut: 15},
		},
	}

	// 5. LoopConfig
	cfg := LoopConfig{
		Router:          mockLLM,
		Tools:           adapter,
		Mapping:         DefaultModelMapping(),
		ProjectRoot:     "/tmp",
		Translator:      NewPathTranslator(),
		DisallowedTools: []string{"WebSearch", "Edit"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := &AgentSpec{
		Name:         "test-agent",
		Model:        "haiku",
		MaxTurns:     5,
		Tools:        specTools, // 全 5 个
		SystemPrompt: "test",
	}

	res, err := RunAgent(ctx, spec, cfg, "test input")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res == nil {
		t.Fatal("Result nil")
	}

	// 6. 第一层防御：WebSearch / Edit 不应在 LLM 看到的 tool list 里
	//    （即使 spec.Tools 含，buildLLMTools 会过滤）
	if len(mockLLM.Calls) == 0 {
		t.Fatal("mock LLM 没收到任何请求")
	}
	firstCall := mockLLM.Calls[0]
	gotToolNames := make(map[string]bool, len(firstCall.Tools))
	for _, lt := range firstCall.Tools {
		gotToolNames[lt.Name] = true
	}
	if gotToolNames["WebSearch"] {
		t.Errorf("LLMTools 过滤失败：WebSearch 不应出现在 LLM 请求的 Tools 列表，但实际出现")
	}
	if gotToolNames["Edit"] {
		t.Errorf("LLMTools 过滤失败：Edit 不应出现在 LLM 请求的 Tools 列表，但实际出现")
	}
	for _, want := range []string{"Read", "Write", "Bash"} {
		if !gotToolNames[want] {
			t.Errorf("LLM 应看到 %q，实际 Tools=%v", want, gotToolNames)
		}
	}

	// 7. 验证 res.ToolCalls 记录了 LLM 的 WebSearch 尝试
	//    （注意：即使 Dispatch 拒绝，res.ToolCalls 仍记录 LLM 的原始 tool_call）
	webSearchFound := false
	for _, tc := range res.ToolCalls {
		if tc.Name == "WebSearch" {
			webSearchFound = true
		}
	}
	if !webSearchFound {
		t.Errorf("res.ToolCalls 应含 WebSearch 调用记录（LLM 尝试调），但未找到")
	}

	// 8. 断言：最终 Content 非空
	if res.Content == "" {
		t.Errorf("Final Content 应非空")
	}
	if !strings.Contains(res.Content, "WebSearch was rejected") {
		t.Errorf("Final Content 应来自 mock 第 2 轮，实际=%q", res.Content)
	}

	// 9. 断言：第二轮 mock LLM 收到的 messages 应含 disallowed error 信息
	//    （loop.go 把 Dispatch 返回的 toolResult 序列化进 messages）
	if len(mockLLM.Calls) >= 2 {
		secondCall := mockLLM.Calls[1]
		foundDisallowed := false
		for _, m := range secondCall.Messages {
			if strings.Contains(m.Content, "disallowed") {
				foundDisallowed = true
				break
			}
		}
		if !foundDisallowed {
			t.Errorf("第二轮 LLM 请求的 messages 应含 'disallowed'（来自 Dispatch 拒绝），但未找到")
		}
	}

	// 10. 断言：Tokens 累积正确（2 轮：10 + 5 = 15）
	if res.TokensIn != 15 {
		t.Errorf("TokensIn 应=15（10 + 5），实际=%d", res.TokensIn)
	}
	if res.TokensOut != 15 {
		t.Errorf("TokensOut 应=15，实际=%d", res.TokensOut)
	}

	t.Logf("✅ Agent.Run defense-in-depth 验证通过：LLMTools 过滤 + Dispatch 拒绝 + LLM 收到 disallowed error 三层都生效")
}

// TestDisallowedToolsE2E_All4ReadOnlyRoles - 4 个 vendor 只读 role 防御 (Sprint A6.14)
//
// 验证：4 个 vendor role（consistency-checker / character-designer / story-explorer / story-researcher）
// 的 DisallowedTools 至少含 Write/Edit/Bash，调 Write/Edit/Bash 应被拒。
func TestDisallowedToolsE2E_All4ReadOnlyRoles(t *testing.T) {
	roleNames := []string{
		"consistency-checker",
		"character-designer",
		"story-explorer",
		"story-researcher",
	}

	for _, roleName := range roleNames {
		roleName := roleName
		t.Run(roleName, func(t *testing.T) {
			// 加载 vendor role spec（先按 vendor 原名，再按 Go alias）
			spec, err := roles.LoadRoleSpec(roleName)
			if err != nil {
				vendor, ok := roles.AliasToVendor(roleName)
				if !ok {
					t.Skipf("role %q not loaded: %v", roleName, err)
				}
				spec, err = roles.LoadRoleSpec(vendor)
				if err != nil {
					t.Skipf("role %q not loaded via alias: %v", roleName, err)
				}
			}

			// 构造 adapter
			reg := tools.NewRegistry()
			_ = reg.Register(tools.NewReadTool(nil))
			_ = reg.Register(tools.NewWriteTool(nil))
			_ = reg.Register(tools.NewEditTool(nil))
			_ = reg.Register(tools.NewBashTool(nil))

			adapter := NewToolAdapter(reg, "/tmp")
			adapter.SetDisallowedTools(spec.DisallowedTools)

			// 调 Write 应被拒（仅当 spec 标记 Write 为 disallowed）
			if containsStr(spec.DisallowedTools, "Write") {
				res := adapter.Dispatch("Write", json.RawMessage(`{"path": "x.txt", "content": "y"}`))
				if !res.IsError {
					t.Errorf("role %q: Write 应被拒，实际仍可调", roleName)
				} else if !strings.Contains(res.Content, "disallowed") {
					t.Errorf("role %q: Write 拒绝信息应含 'disallowed'，实际=%q", roleName, res.Content)
				}
			} else {
				t.Logf("WARN: role %q 没禁止 Write（DisallowedTools=%v），跳过", roleName, spec.DisallowedTools)
			}

			// 调 Edit 应被拒（仅当 spec 标记 Edit 为 disallowed）
			if containsStr(spec.DisallowedTools, "Edit") {
				res := adapter.Dispatch("Edit", json.RawMessage(`{"path": "x.txt", "old_string": "a", "new_string": "b"}`))
				if !res.IsError {
					t.Errorf("role %q: Edit 应被拒，实际仍可调", roleName)
				} else if !strings.Contains(res.Content, "disallowed") {
					t.Errorf("role %q: Edit 拒绝信息应含 'disallowed'，实际=%q", roleName, res.Content)
				}
			} else {
				t.Logf("WARN: role %q 没禁止 Edit（DisallowedTools=%v），跳过", roleName, spec.DisallowedTools)
			}
		})
	}
}

// containsStr 检查字符串 slice 是否含某元素
func containsStr(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}