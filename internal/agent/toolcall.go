package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ourvps1688/novel2all-go/internal/agent/tools"
	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// ToolAdapter 把 agent/tools.Tool 适配为 llm.Tool (Sprint A5.2)
//
// llm.ChatWithTools 接受 llm.Tool（带 Handler func），
// agent/tools.Tool 是结构化接口（Execute 方法 + 沙箱 ExecContext）。
//
// 这个 adapter 把后者包成前者，使 Agent.Run() 能直接调 LLM 调 tool。
type ToolAdapter struct {
	mu              sync.RWMutex
	registry        *tools.Registry
	sandbox         *tools.ExecContext
	disallowedTools map[string]bool // A5.16 防御层
}

// NewToolAdapter 构造 adapter
func NewToolAdapter(reg *tools.Registry, sandboxRoot string) *ToolAdapter {
	return &ToolAdapter{
		registry: reg,
		sandbox: &tools.ExecContext{
			Root:       sandboxRoot,
			WorkingDir: sandboxRoot,
			State:      make(map[string]any),
		},
	}
}

// LLMTools 把 agent/tools.Registry 转成 []llm.Tool 列表（供 LLM 调）
//
// vendor role 的 spec.Tools 字段（来自 RoleSpec）会指定允许的工具列表。
// 这里返回 spec.Tools 列出的工具，但先过滤掉 a.disallowedTools 里的（A5.14/A6.14 一致性）。
// 未注册的 tool 跳过（vendor role 可能引用未实现的 tool）。
func (a *ToolAdapter) LLMTools(toolNames []string) []llm.Tool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]llm.Tool, 0, len(toolNames))
	for _, name := range toolNames {
		if _, blocked := a.disallowedTools[name]; blocked {
			continue // A5.14/A6.14: LLMTools 也过滤 disallowed（与 Dispatch 一致）
		}
		t, err := a.registry.Get(name)
		if err != nil {
			continue // 未注册的 tool 跳过（vendor role 可能引用未实现的 tool）
		}
		// 闭包捕获 name + adapter + sandbox
		toolCopy := t
		sandboxCopy := a.sandbox

		out = append(out, llm.Tool{
			Name:       toolCopy.Name(),
			Parameters: toolCopy.InputSchema(),
			Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
				// ctx 是 llm 传入的 context.Context（可取消）
				// tools.Tool.Execute 接受 *tools.ExecContext（沙箱），不直接用 ctx
				_ = ctx
				result, _ := toolCopy.Execute(sandboxCopy, args)
				if result.IsError {
					return nil, &toolError{msg: result.Content}
				}
				return result.Content, nil
			},
			Description: toolCopy.Description(),
		})
	}
	return out
}

// Dispatch 按 tool_call 名称 + 参数执行实际工具
//
// A5.16 defense-in-depth：先检查 tool 是否在 disallowed set（如有），
// 防止 buildLLLSS 之外的绕过路径（如 LLM 恶意调 disallowed tool）。
func (a *ToolAdapter) Dispatch(toolName string, args json.RawMessage) tools.Result {
	a.mu.RLock()
	disallowed := a.disallowedTools
	a.mu.RUnlock()

	if _, blocked := disallowed[toolName]; blocked {
		return tools.ErrorResult(fmt.Sprintf(
			"ToolAdapter: tool %q is disallowed by vendor role spec", toolName))
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	t, err := a.registry.Get(toolName)
	if err != nil {
		return tools.ErrorResult("ToolAdapter: " + err.Error())
	}
	result, _ := t.Execute(a.sandbox, args)
	return result
}

// SetDisallowedTools 设置 agent-level DisallowedTools（A5.16 防御层）
func (a *ToolAdapter) SetDisallowedTools(disallowed []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.disallowedTools = make(map[string]bool, len(disallowed))
	for _, name := range disallowed {
		a.disallowedTools[name] = true
	}
}

// toolError tool 调用失败返回的错误（带 msg 字段）
type toolError struct{ msg string }

func (e *toolError) Error() string { return e.msg }

// Sandbox 返回当前 sandbox（Agent.Run 用）
func (a *ToolAdapter) Sandbox() *tools.ExecContext {
	return a.sandbox
}

// SetSandboxRoot 更新 sandbox 根目录
func (a *ToolAdapter) SetSandboxRoot(root string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sandbox.Root = root
	a.sandbox.WorkingDir = root
}
