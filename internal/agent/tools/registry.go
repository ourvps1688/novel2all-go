package tools

import (
	"fmt"
	"sort"
	"sync"
)

// Registry Tool 注册表（Sprint A2.8）
//
// 集中管理所有 Tool 实例。线程安全（sync.RWMutex）。
// Agent framework 通过 Registry 查找 + 调用 Tool。
//
// 用法：
//   reg := tools.NewRegistry()
//   reg.Register(NewReadTool(sandbox))
//   reg.Register(NewBashTool(sandbox))
//   tool, _ := reg.Get("Read")
//   result, err := tool.Execute(ctx, []byte(`{"path": "..."}`))
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry 创建空 Registry
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register 注册一个 Tool（同名重复注册会报错）
func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("registry: nil tool")
	}
	name := tool.Name()
	if name == "" {
		return fmt.Errorf("registry: tool has empty Name()")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("registry: tool %q already registered", name)
	}
	r.tools[name] = tool
	return nil
}

// MustRegister 注册失败时 panic（init 阶段用）
func (r *Registry) MustRegister(tool Tool) {
	if err := r.Register(tool); err != nil {
		panic(err)
	}
}

// Get 按 name 查 Tool
func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("registry: tool %q not found", name)
	}
	return t, nil
}

// Has 检查 tool 是否已注册
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// List 列出所有 tool names（按字典序）
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Count 返回已注册数量
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Dispatch 按 name 调用 tool（Registry.Execute 的便利方法）
func (r *Registry) Dispatch(name string, execCtx *ExecContext, input []byte) (Result, error) {
	t, err := r.Get(name)
	if err != nil {
		return Result{}, err
	}
	return t.Execute(execCtx, input)
}

// ToolSchemas 返回所有 tool 的 (name, schema) 列表（用于 LLM tool_use）
type ToolSchema struct {
	Name        string
	Description string
	InputSchema []byte
}

// Schemas 列出所有 tool 的 schema（用于 LLM 注册）
func (r *Registry) Schemas() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolSchema, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	// 按 name 排序（稳定输出）
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
