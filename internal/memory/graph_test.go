// graph_test.go 测试 MemoryGraph.
package memory

import (
	"strings"
	"testing"
)

// 常量定义避免 goconst lint.
const (
	charAlice = "char:alice"
	charBob   = "char:bob"
	charCarol = "char:carol"
	locTown   = "loc:town"
)

func newTestGraph() *MemoryGraph {
	return NewMemoryGraph(&GraphData{
		Nodes: []GraphNode{
			{ID: charAlice, Type: NodeCharacter, Name: "Alice"},
			{ID: charBob, Type: NodeCharacter, Name: "Bob"},
			{ID: charCarol, Type: NodeCharacter, Name: "Carol"},
			{ID: locTown, Type: NodeLocation, Name: "Town"},
		},
		Edges: []GraphEdge{
			{FromID: charAlice, ToID: charBob, Type: EdgeRelatedTo},
			{FromID: charBob, ToID: charCarol, Type: EdgeRelatedTo},
			{FromID: charAlice, ToID: locTown, Type: EdgeLocatedIn},
		},
	})
}

func TestMemoryGraph_AddNodeEdge(t *testing.T) {
	g := NewMemoryGraph(nil)
	g.AddNode(GraphNode{ID: "char:x", Name: "X"})
	g.AddNode(GraphNode{ID: "char:x", Name: "X Duplicate"})
	if len(g.Data().Nodes) != 1 {
		t.Errorf("dedup nodes: got %d", len(g.Data().Nodes))
	}

	g.AddEdge(GraphEdge{FromID: "char:x", ToID: "char:y", Type: EdgeRelatedTo})
	g.AddEdge(GraphEdge{FromID: "char:x", ToID: "char:y", Type: EdgeRelatedTo}) // dup
	if len(g.Data().Edges) != 1 {
		t.Errorf("dedup edges: got %d", len(g.Data().Edges))
	}
}

func TestMemoryGraph_RemoveNode(t *testing.T) {
	g := newTestGraph()
	if !g.RemoveNode("char:bob") {
		t.Fatal("RemoveNode should return true")
	}
	if g.HasNode("char:bob") {
		t.Error("bob should be removed")
	}
	// edges with bob should also be gone
	for _, e := range g.IterEdges() {
		if e.FromID == "char:bob" || e.ToID == "char:bob" {
			t.Errorf("edge with bob not removed: %+v", e)
		}
	}
}

func TestMemoryGraph_GetNeighbors(t *testing.T) {
	g := newTestGraph()
	all := g.GetNeighbors("char:alice")
	if len(all) != 2 {
		t.Errorf("alice neighbors = %d, want 2 (bob + town)", len(all))
	}

	filtered := g.GetNeighbors("char:alice", EdgeRelatedTo)
	if len(filtered) != 1 {
		t.Errorf("alice RELATED_TO neighbors = %d, want 1 (bob)", len(filtered))
	}
}

func TestMemoryGraph_GetPath(t *testing.T) {
	g := newTestGraph()
	// alice → carol (via bob, 2 hops)
	path := g.GetPath("char:alice", "char:carol", 0)
	if len(path) != 2 {
		t.Errorf("path length = %d, want 2", len(path))
	}
	if path[0].FromID != "char:alice" || path[0].ToID != "char:bob" {
		t.Errorf("first edge wrong: %+v", path[0])
	}
	if path[1].FromID != "char:bob" || path[1].ToID != "char:carol" {
		t.Errorf("second edge wrong: %+v", path[1])
	}

	// maxHops=1 should fail (alice→carol needs 2)
	if got := g.GetPath("char:alice", "char:carol", 1); got != nil {
		t.Errorf("maxHops=1 should return nil, got %v", got)
	}

	// disconnected
	if got := g.GetPath("char:alice", "char:nonexistent", 0); got != nil {
		t.Errorf("nonexistent should return nil")
	}
}

func TestMemoryGraph_ToFromJSON(t *testing.T) {
	g := newTestGraph()
	json, err := g.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(json, "char:alice") {
		t.Error("JSON should contain char:alice")
	}

	g2, err := FromJSON(json)
	if err != nil {
		t.Fatal(err)
	}
	nodes, edges := g2.Count()
	if nodes != 4 || edges != 3 {
		t.Errorf("after roundtrip: nodes=%d edges=%d, want 4/3", nodes, edges)
	}
}

func TestMemoryGraph_FromState(t *testing.T) {
	state := newEmptyState("test")
	state.Graph = nil // 测试 nil fallback
	g := FromState(state)
	if g == nil {
		t.Fatal("FromState should not return nil")
	}
	nodes, _ := g.Count()
	if nodes != 0 {
		t.Errorf("empty graph nodes = %d, want 0", nodes)
	}
}
