package agent

import (
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/roles"
)

// Dispatcher Stage → Agent 映射 (Sprint A5.5)
//
// vendor 8 节 short_mode stage runner 需要按 stage 名找对应 agent。
// 典型 stage: "outline" / "chapter_write" / "consistency_check" / "character_extract"
// 等，对应 vendor role 名 (story-architect / narrative-writer / 等)。
type Dispatcher struct {
	// agents vendor 原名 → 已加载的 Agent 实例
	agents map[string]*Agent
}

// NewDispatcher 构造 dispatcher（传入加载好的 agents map）
func NewDispatcher(agents map[string]*Agent) *Dispatcher {
	return &Dispatcher{agents: agents}
}

// DispatchByStage 按 short_mode stage 名找对应 agent（向后兼容）
//
// 优先匹配 stage → role 的经典映射：
//   - "outline" → story-architect
//   - "chapter_write" → narrative-writer
//   - "consistency_check" → consistency-checker
//   - "character_extract" → chapter-extractor
//   - "chapter_review" → character-designer
//
// 如未匹配，回退按 stage 名（可能直接是 vendor role 名）查找。
func (d *Dispatcher) DispatchByStage(stage string) (*Agent, error) {
	if stage == "" {
		return nil, fmt.Errorf("dispatcher: empty stage")
	}

	// 1. 经典 stage → role 映射
	stageToRole := map[string]string{
		"outline":           "story-architect",
		"chapter_write":     "narrative-writer",
		"chapter_review":    "character-designer",
		"consistency_check": "consistency-checker",
		"character_extract": "chapter-extractor",
		"story_explore":     "story-explorer",
		"story_research":    "story-researcher",
	}
	if roleName, ok := stageToRole[stage]; ok {
		return d.lookup(roleName)
	}

	// 2. 回退：stage 名直接当 vendor role 名
	return d.lookup(stage)
}

// DispatchByRole 按 vendor 原名找 agent
func (d *Dispatcher) DispatchByRole(roleName string) (*Agent, error) {
	return d.lookup(roleName)
}

// DispatchByAlias 按 Go alias 找 agent（向后兼容 Sprint 35 旧 const 名）
func (d *Dispatcher) DispatchByAlias(alias roles.Role) (*Agent, error) {
	// alias → vendor 原名
	vendor, ok := roles.AliasToVendor(string(alias))
	if !ok {
		// 直接当 vendor 原名试
		return d.lookup(string(alias))
	}
	return d.lookup(vendor)
}

// lookup 内部 helper（统一处理 not-found）
func (d *Dispatcher) lookup(roleName string) (*Agent, error) {
	if d == nil || d.agents == nil {
		return nil, fmt.Errorf("dispatcher: no agents registered")
	}
	a, ok := d.agents[roleName]
	if !ok {
		return nil, fmt.Errorf("dispatcher: agent %q not found in registry", roleName)
	}
	return a, nil
}

// AvailableRoles 返回 dispatcher 可分发的所有 agent 名字（按字典序）
func (d *Dispatcher) AvailableRoles() []string {
	if d == nil || d.agents == nil {
		return nil
	}
	out := make([]string, 0, len(d.agents))
	for name := range d.agents {
		out = append(out, name)
	}
	// 简单选择排序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
