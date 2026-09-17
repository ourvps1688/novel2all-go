// Package memory 提供 novel2all-go 长记忆系统 (Sprint 22+).
//
// 设计:
//   - 5 层 memory (CORE/CHARACTER/RECENT/EVENT/GRAPH) 对应不同检索策略
//   - 类型用 struct + JSON tag (代替 Python pydantic BaseModel)
//   - 数据通过 TrackingState 持久化到 _tracking-state.json
//
// 参考 Python core/memory/types.py V0.21+.
package memory

import "time"

// MemoryLayer 5 层记忆层级.
//
//nolint:revive // type name stutters but matches Python source
type MemoryLayer string

const (
	LayerCore      MemoryLayer = "core"      // 永远加载: 核心设定/文风基线
	LayerCharacter MemoryLayer = "character" // 按需加载: 本章涉及角色状态
	LayerRecent    MemoryLayer = "recent"    // 滑动窗口: 最近 N 章摘要
	LayerEvent     MemoryLayer = "event"     // 向量检索: 相关历史事件
	LayerGraph     MemoryLayer = "graph"     // 知识图谱: tool call 按需查询
)

// MemoryItem 一条记忆.
//
//nolint:revive // type name stutters but matches Python source
type MemoryItem struct {
	Content    string      `json:"content"`
	Source     string      `json:"source"`
	Layer      MemoryLayer `json:"layer"`
	Relevance  float64     `json:"relevance"` // 0-1
	TokenCount int         `json:"token_count"`
	Timestamp  *time.Time  `json:"timestamp,omitempty"`
}

// MemoryContext 一次写作任务加载的完整 memory.
//
//nolint:revive // type name stutters but matches Python source
type MemoryContext struct {
	Core      []MemoryItem `json:"core"`
	Character []MemoryItem `json:"character"`
	Recent    []MemoryItem `json:"recent"`
	Events    []MemoryItem `json:"events"`
	Graph     []MemoryItem `json:"graph"`
}

// TotalTokens 统计总 token 数.
func (c *MemoryContext) TotalTokens() int {
	total := 0
	for _, list := range [][]MemoryItem{c.Core, c.Character, c.Recent, c.Events, c.Graph} {
		for _, item := range list {
			total += item.TokenCount
		}
	}
	return total
}

// ToSystemSections 组装成 LLM system message 段落.
//
//nolint:gocyclo // 5 layer branches is natural for memory assembly
func (c *MemoryContext) ToSystemSections() []string {
	var sections []string
	sections = appendSection(sections, "# 核心设定", c.Core)
	sections = appendSection(sections, "# 角色状态", c.Character)
	sections = appendSection(sections, "# 最近章节", c.Recent)
	sections = appendSection(sections, "# 相关历史事件", c.Events)
	sections = appendSection(sections, "# 知识图谱片段", c.Graph)
	return sections
}

// appendSection append 1 section (header + items).
func appendSection(sections []string, header string, items []MemoryItem) []string {
	if len(items) == 0 {
		return sections
	}
	s := header + "\n"
	for i, item := range items {
		if i > 0 {
			s += "\n\n"
		}
		s += item.Content
	}
	return append(sections, s)
}

// MemoryConfig memory 加载策略.
//
//nolint:revive // type name stutters but matches Python source
type MemoryConfig struct {
	CoreTokenBudget       int  `json:"core_token_budget"`        // 默认 3000
	CharacterTokenBudget  int  `json:"character_token_budget"`   // 默认 4000
	RecentChapterCount    int  `json:"recent_chapter_count"`     // 默认 5
	RecentTokenBudget     int  `json:"recent_token_budget"`      // 默认 6000
	EventTopK             int  `json:"event_top_k"`              // 默认 8
	EventTokenBudget      int  `json:"event_token_budget"`       // 默认 5000
	AutoExtractAfterWrite bool `json:"auto_extract_after_write"` // 默认 true
	AutoEmbedAfterWrite   bool `json:"auto_embed_after_write"`   // 默认 true
	PreWriteCheckBlocking bool `json:"pre_write_check_blocking"` // 默认 true
}

// DefaultMemoryConfig 默认配置 (对齐 Python V0.21+).
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		CoreTokenBudget:       3000,
		CharacterTokenBudget:  4000,
		RecentChapterCount:    5,
		RecentTokenBudget:     6000,
		EventTopK:             8,
		EventTokenBudget:      5000,
		AutoExtractAfterWrite: true,
		AutoEmbedAfterWrite:   true,
		PreWriteCheckBlocking: true,
	}
}

// === L5 知识图谱 (V0.22) ===

// NodeType 知识图谱节点类型.
type NodeType string

const (
	NodeCharacter     NodeType = "Character"
	NodeLocation      NodeType = "Location"
	NodeForeshadowing NodeType = "Foreshadowing"
	NodeEvent         NodeType = "Event"
	NodeItem          NodeType = "Item"
)

// EdgeType 知识图谱边类型.
type EdgeType string

const (
	EdgeAppearsIn   EdgeType = "appears_in"
	EdgeLocatedIn   EdgeType = "located_in"
	EdgeOwns        EdgeType = "owns"
	EdgeRelatedTo   EdgeType = "related_to"
	EdgeForeshadows EdgeType = "foreshadows"
	EdgeCausedBy    EdgeType = "caused_by"
	EdgeSetIn       EdgeType = "set_in"
)

// GraphNode 知识图谱节点.
//
// ID 格式:
//   - Character: "char:林雷"
//   - Location: "loc:苍茫镇"
//   - Foreshadowing: "fs:bloodline_secret"
//   - Event: "event:awakening_ch2"
//   - Item: "item:玉佩"
type GraphNode struct {
	ID           string   `json:"id"`
	Type         NodeType `json:"type"`
	Name         string   `json:"name"`
	Alive        *bool    `json:"alive,omitempty"`         // 仅 Character
	TypeDetail   *string  `json:"type_detail,omitempty"`   // 仅 Location (city/sect/wild/...)
	IDShort      *string  `json:"id_short,omitempty"`      // 仅 Foreshadowing
	Status       *string  `json:"status,omitempty"`        // 仅 Foreshadowing (active/advanced/revealed)
	InWorldTime  *string  `json:"in_world_time,omitempty"` // 仅 Event
	FirstChapter *int     `json:"first_chapter,omitempty"`
	LastChapter  *int     `json:"last_chapter,omitempty"`
}

// ShortID 返回去掉前缀的短 ID (仅用于显示).
func (n *GraphNode) ShortID() string {
	for i := 0; i < len(n.ID); i++ {
		if n.ID[i] == ':' {
			return n.ID[i+1:]
		}
	}
	return n.ID
}

// GraphEdge 知识图谱边.
type GraphEdge struct {
	FromID string   `json:"from_id"`
	ToID   string   `json:"to_id"`
	Type   EdgeType `json:"type"`
}

// GraphData 图谱完整数据 (nodes + edges).
type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// NodeExists 检查 node ID 是否在 graph 中.
func (g *GraphData) NodeExists(id string) bool {
	for _, n := range g.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// EdgeExists 检查 (from, to, type) 边是否在 graph 中.
func (g *GraphData) EdgeExists(from, to string, edgeType EdgeType) bool {
	for _, e := range g.Edges {
		if e.FromID == from && e.ToID == to && e.Type == edgeType {
			return true
		}
	}
	return false
}

// AddNode 添加 node (如已存在则忽略).
func (g *GraphData) AddNode(n GraphNode) {
	if !g.NodeExists(n.ID) {
		g.Nodes = append(g.Nodes, n)
	}
}

// AddEdge 添加 edge (如已存在则忽略).
func (g *GraphData) AddEdge(e GraphEdge) {
	if !g.EdgeExists(e.FromID, e.ToID, e.Type) {
		g.Edges = append(g.Edges, e)
	}
}

// === 一致性问题 (verifier + extractor 共享) ===

// ConsistencyIssue 一致性问题 (verifier 输出 + extractor 兜底).
//
// Severity: critical (阻断) / warning (建议) / info (观察).
// Category: character / foreshadowing / timeline / setting / style / ai_smell.
type ContinuityIssue struct {
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Evidence    string `json:"evidence,omitempty"`
	Suggestion  string `json:"suggestion,omitempty"`
}
