// types_test.go 测试 MemoryItem/MemoryContext/MemoryConfig/GraphData.
package memory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMemoryContext_TotalTokens(t *testing.T) {
	c := &MemoryContext{
		Core:      []MemoryItem{{TokenCount: 100}, {TokenCount: 200}},
		Character: []MemoryItem{{TokenCount: 50}},
		Recent:    []MemoryItem{{TokenCount: 1000}},
	}
	if got := c.TotalTokens(); got != 1350 {
		t.Errorf("TotalTokens = %d, want 1350", got)
	}
}

func TestMemoryContext_ToSystemSections(t *testing.T) {
	c := &MemoryContext{
		Core:  []MemoryItem{{Content: "core1"}, {Content: "core2"}},
		Graph: []MemoryItem{{Content: "graph1"}},
	}
	sections := c.ToSystemSections()
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections (core + graph), got %d", len(sections))
	}
	if !strings.Contains(sections[0], "# 核心设定") || !strings.Contains(sections[0], "core1") {
		t.Errorf("core section missing content: %q", sections[0])
	}
	if !strings.Contains(sections[1], "# 知识图谱片段") {
		t.Errorf("graph section missing header: %q", sections[1])
	}
}

func TestMemoryConfig_Default(t *testing.T) {
	c := DefaultMemoryConfig()
	if c.CoreTokenBudget != 3000 {
		t.Errorf("CoreTokenBudget = %d, want 3000", c.CoreTokenBudget)
	}
	if !c.AutoExtractAfterWrite {
		t.Errorf("AutoExtractAfterWrite should be true")
	}
}

func TestGraphNode_ShortID(t *testing.T) {
	n := &GraphNode{ID: "char:林雷"}
	if n.ShortID() != "林雷" {
		t.Errorf("ShortID = %q, want 林雷", n.ShortID())
	}
	n2 := &GraphNode{ID: "no_prefix"}
	if n2.ShortID() != "no_prefix" {
		t.Errorf("ShortID without colon = %q, want no_prefix", n2.ShortID())
	}
}

func TestGraphData_AddNodeEdge(t *testing.T) {
	g := &GraphData{}
	g.AddNode(GraphNode{ID: "char:alice", Name: "Alice"})
	g.AddNode(GraphNode{ID: "char:alice", Name: "Alice Duplicate"}) // dedup
	if len(g.Nodes) != 1 {
		t.Errorf("Nodes after dedup = %d, want 1", len(g.Nodes))
	}

	g.AddEdge(GraphEdge{FromID: "char:alice", ToID: "char:bob", Type: EdgeRelatedTo})
	g.AddEdge(GraphEdge{FromID: "char:alice", ToID: "char:bob", Type: EdgeRelatedTo}) // dedup
	if len(g.Edges) != 1 {
		t.Errorf("Edges after dedup = %d, want 1", len(g.Edges))
	}

	g.AddEdge(GraphEdge{FromID: "char:alice", ToID: "char:carol", Type: EdgeRelatedTo}) // different target
	if len(g.Edges) != 2 {
		t.Errorf("Edges with different target = %d, want 2", len(g.Edges))
	}
}

func TestGraphData_NodeIndex(t *testing.T) {
	g := &GraphData{}
	g.AddNode(GraphNode{ID: "a"})
	g.AddNode(GraphNode{ID: "b"})
	g.AddNode(GraphNode{ID: "c"})

	idx := g.NodeIndex()
	if idx["a"] != 0 || idx["b"] != 1 || idx["c"] != 2 {
		t.Errorf("NodeIndex wrong: %v", idx)
	}
	if _, ok := idx["nonexistent"]; ok {
		t.Error("nonexistent should not be in index")
	}
}

func TestGraphData_EdgeIndex(t *testing.T) {
	g := &GraphData{}
	g.AddEdge(GraphEdge{FromID: "a", ToID: "b", Type: EdgeRelatedTo})
	g.AddEdge(GraphEdge{FromID: "b", ToID: "c", Type: EdgeLocatedIn})

	idx := g.EdgeIndex()
	if len(idx) != 2 {
		t.Errorf("EdgeIndex size = %d, want 2", len(idx))
	}
	key1 := "a\x00b\x00" + string(EdgeRelatedTo)
	key2 := "b\x00c\x00" + string(EdgeLocatedIn)
	if idx[key1] != 0 || idx[key2] != 1 {
		t.Errorf("EdgeIndex wrong: %v", idx)
	}
}

func TestMemoryContext_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	original := &MemoryContext{
		Core: []MemoryItem{{
			Content: "core", Layer: LayerCore, TokenCount: 10, Timestamp: &now,
		}},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MemoryContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Core) != 1 || decoded.Core[0].Content != "core" {
		t.Errorf("roundtrip failed: %+v", decoded)
	}
	if decoded.Core[0].Timestamp == nil {
		t.Error("timestamp lost in roundtrip")
	}
}

func TestMemoryContext_IterAll(t *testing.T) {
	c := &MemoryContext{
		Core:      []MemoryItem{{Content: "c1"}, {Content: "c2"}},
		Character: []MemoryItem{{Content: "ch1"}},
		Recent:    []MemoryItem{{Content: "r1"}},
		Events:    []MemoryItem{{Content: "e1"}},
		Graph:     []MemoryItem{{Content: "g1"}},
	}
	all := c.IterAll()
	if len(all) != 6 {
		t.Errorf("IterAll length = %d, want 6", len(all))
	}
	// 验证顺序: Core → Character → Recent → Events → Graph
	want := []string{"c1", "c2", "ch1", "r1", "e1", "g1"}
	for i, item := range all {
		if item.Content != want[i] {
			t.Errorf("IterAll[%d] = %q, want %q", i, item.Content, want[i])
		}
	}
}

func TestMemoryContext_IterAll_Empty(t *testing.T) {
	c := &MemoryContext{}
	all := c.IterAll()
	if len(all) != 0 {
		t.Errorf("empty IterAll length = %d, want 0", len(all))
	}
}
