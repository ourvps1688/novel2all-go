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
