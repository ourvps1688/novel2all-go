package agent

import (
	"fmt"
	"sync"
)

// Registry Agent 注册表 (Sprint A1.5)
//
// 按 name 查 Agent 实例。线程安全（sync.RWMutex）。
//
// 用法：
//
//	reg := agent.NewRegistry()
//	reg.Register(spec)              // 注册一个 AgentSpec
//	agent, err := reg.Get("story-architect")
//	agents := reg.List()             // 列出所有
type Registry struct {
	mu    sync.RWMutex
	items map[string]*Agent
}

// NewRegistry 创建空 Registry
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]*Agent)}
}

// Register 注册一个 Agent（从 spec 构造 Agent）
//
// 同名重复注册会覆盖前者 + 返回 error（防止误用）。
// Sprint A3 阶段会扩展支持从 embed.FS 批量加载。
func (r *Registry) Register(spec *AgentSpec) error {
	if spec == nil {
		return fmt.Errorf("registry: nil spec")
	}
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.items[spec.Name]; exists {
		return fmt.Errorf("registry: agent %q already registered", spec.Name)
	}
	r.items[spec.Name] = NewAgent(spec)
	return nil
}

// MustRegister 注册失败时 panic（仅用于 init 阶段的批量加载）
func (r *Registry) MustRegister(spec *AgentSpec) {
	if err := r.Register(spec); err != nil {
		panic(err)
	}
}

// Get 按 name 查 Agent
func (r *Registry) Get(name string) (*Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.items[name]
	if !ok {
		return nil, fmt.Errorf("registry: agent %q not found", name)
	}
	return a, nil
}

// Has 检查 agent 是否已注册
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.items[name]
	return ok
}

// List 列出所有已注册的 agent names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]string, 0, len(r.items))
	for name := range r.items {
		out = append(out, name)
	}
	return out
}

// Count 返回已注册数量
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}

// LoadFromDir 批量从目录加载所有 role .md（便利方法）
func (r *Registry) LoadFromDir(dir string) error {
	specs, err := LoadAgentSpecFromDir(dir)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if err := r.Register(spec); err != nil {
			return fmt.Errorf("load from dir %q: %w", dir, err)
		}
	}
	return nil
}
