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
	if !g.RemoveNode(charBob) {
		t.Fatal("RemoveNode should return true")
	}
	if g.HasNode(charBob) {
		t.Error("bob should be removed")
	}
	// edges with bob should also be gone
	for _, e := range g.IterEdges() {
		if e.FromID == charBob || e.ToID == charBob {
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
	if path[0].FromID != charAlice || path[0].ToID != charBob {
		t.Errorf("first edge wrong: %+v", path[0])
	}
	if path[1].FromID != charBob || path[1].ToID != charCarol {
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

func TestMemoryGraph_NodeEdgeCount(t *testing.T) {
	g := newTestGraph()
	if g.NodeCount() != 4 {
		t.Errorf("NodeCount = %d, want 4", g.NodeCount())
	}
	if g.EdgeCount() != 3 {
		t.Errorf("EdgeCount = %d, want 3", g.EdgeCount())
	}
}

func TestMemoryGraph_Neighbors(t *testing.T) {
	g := newTestGraph()
	// alice 有 2 个邻居 (bob + town), 但 alice-bob-edge 出现 1 次在 adj
	nbrs := g.Neighbors("char:alice")
	if len(nbrs) != 2 {
		t.Errorf("alice neighbors = %d, want 2", len(nbrs))
	}
}

func TestMemoryGraph_Subgraph(t *testing.T) {
	g := newTestGraph()
	sub := g.Subgraph([]string{"char:alice", "char:bob"})
	if sub.NodeCount() != 2 {
		t.Errorf("subgraph nodes = %d, want 2", sub.NodeCount())
	}
	// alice-bob edge 应该保留, 但 bob-carol 和 alice-town 不保留
	if sub.EdgeCount() != 1 {
		t.Errorf("subgraph edges = %d, want 1", sub.EdgeCount())
	}
}

func TestMemoryGraph_MergeGraph(t *testing.T) {
	g1 := NewMemoryGraph(nil)
	g1.AddNode(GraphNode{ID: "char:a", Name: "A"})
	g1.AddNode(GraphNode{ID: "char:b", Name: "B"}) // g1 也有 b (dup 测试)
	g1.AddEdge(GraphEdge{FromID: "char:a", ToID: "char:b", Type: EdgeRelatedTo})

	g2 := NewMemoryGraph(nil)
	g2.AddNode(GraphNode{ID: "char:b", Name: "B"}) // dup
	g2.AddNode(GraphNode{ID: "char:c", Name: "C"}) // new
	g2.AddEdge(GraphEdge{FromID: "char:b", ToID: "char:c", Type: EdgeRelatedTo})

	newNodes, newEdges := g1.MergeGraph(g2)
	if newNodes != 1 {
		t.Errorf("new nodes = %d, want 1 (only c)", newNodes)
	}
	if newEdges != 1 {
		t.Errorf("new edges = %d, want 1", newEdges)
	}
	if g1.NodeCount() != 3 {
		t.Errorf("after merge nodes = %d, want 3", g1.NodeCount())
	}
	if g1.EdgeCount() != 2 {
		t.Errorf("after merge edges = %d, want 2", g1.EdgeCount())
	}
}

func TestMemoryGraph_BFSPaths_Single(t *testing.T) {
	g := newTestGraph()
	paths := g.BFSPaths("char:alice", "char:carol", 0)
	if len(paths) != 1 {
		t.Errorf("alice→carol paths = %d, want 1", len(paths))
	}
	// path = alice → bob → carol
	if len(paths) > 0 && len(paths[0]) != 3 {
		t.Errorf("path length = %d, want 3", len(paths[0]))
	}
}

func TestMemoryGraph_BFSPaths_Multiple(t *testing.T) {
	// 构造: A - B, A - C, B - D, C - D (2 条 A→D 路径: A-B-D, A-C-D)
	g := NewMemoryGraph(nil)
	g.AddNode(GraphNode{ID: "A"})
	g.AddNode(GraphNode{ID: "B"})
	g.AddNode(GraphNode{ID: "C"})
	g.AddNode(GraphNode{ID: "D"})
	g.AddEdge(GraphEdge{FromID: "A", ToID: "B", Type: EdgeRelatedTo})
	g.AddEdge(GraphEdge{FromID: "A", ToID: "C", Type: EdgeRelatedTo})
	g.AddEdge(GraphEdge{FromID: "B", ToID: "D", Type: EdgeRelatedTo})
	g.AddEdge(GraphEdge{FromID: "C", ToID: "D", Type: EdgeRelatedTo})

	paths := g.BFSPaths("A", "D", 0)
	if len(paths) != 2 {
		t.Errorf("A→D paths = %d, want 2", len(paths))
	}
}

func TestMemoryGraph_BFSPaths_NoPath(t *testing.T) {
	g := newTestGraph()
	paths := g.BFSPaths("char:alice", "char:nonexistent", 0)
	if paths != nil {
		t.Errorf("nonexistent should return nil, got %v", paths)
	}
}
