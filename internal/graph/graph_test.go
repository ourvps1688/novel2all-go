package graph

import (
	"math"
	"sort"
	"testing"
)

func TestGraph_AddNode(t *testing.T) {
	g := NewGraph()
	if err := g.AddNode("alice", "Alice", nil); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !g.HasNode("alice") {
		t.Error("expected alice to exist")
	}
	if g.NodeCount() != 1 {
		t.Errorf("expected 1 node, got %d", g.NodeCount())
	}
	// 重复 add 应该 no-op (upsert)
	if err := g.AddNode("alice", "Alice2", nil); err != nil {
		t.Errorf("upsert: %v", err)
	}
	if g.NodeCount() != 1 {
		t.Errorf("expected 1 node after upsert, got %d", g.NodeCount())
	}
}

func TestGraph_AddNode_EmptyID(t *testing.T) {
	g := NewGraph()
	if err := g.AddNode("", "", nil); err == nil {
		t.Error("expected error for empty id")
	}
}

func TestGraph_AddEdge(t *testing.T) {
	g := NewGraph()
	if err := g.AddEdge("a", "b", 0); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !g.HasEdge("a", "b") || !g.HasEdge("b", "a") {
		t.Error("expected undirected edge")
	}
	if g.EdgeCount() != 1 {
		t.Errorf("expected 1 edge, got %d", g.EdgeCount())
	}
	// auto-create nodes
	if !g.HasNode("a") || !g.HasNode("b") {
		t.Error("expected auto-created nodes")
	}
}

func TestGraph_AddEdge_NegativeWeight(t *testing.T) {
	g := NewGraph()
	if err := g.AddEdge("a", "b", -1); err == nil {
		t.Error("expected error for negative weight")
	}
}

func TestGraph_Neighbors(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 0)
	_ = g.AddEdge("a", "c", 0)
	_ = g.AddEdge("a", "d", 0)
	n, err := g.Neighbors("a")
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(n) != 3 {
		t.Errorf("expected 3 neighbors, got %d", len(n))
	}
	expected := []string{"b", "c", "d"}
	if !equalStrings(n, expected) {
		t.Errorf("expected %v, got %v", expected, n)
	}
}

func TestGraph_Neighbors_NotFound(t *testing.T) {
	g := NewGraph()
	_, err := g.Neighbors("nonexistent")
	if err != ErrNodeNotFound {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestGraph_Nodes_Edges(t *testing.T) {
	g := NewGraph()
	_ = g.AddNode("a", "", nil)
	_ = g.AddNode("b", "", nil)
	_ = g.AddNode("c", "", nil)
	_ = g.AddEdge("a", "b", 0)
	_ = g.AddEdge("b", "c", 0)
	nodes := g.Nodes()
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}
	edges := g.Edges()
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
}

func TestGraph_Clear(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 0)
	g.Clear()
	if g.NodeCount() != 0 {
		t.Errorf("expected 0 nodes after clear, got %d", g.NodeCount())
	}
}

func TestBFS_Simple(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 0)
	_ = g.AddEdge("b", "c", 0)
	_ = g.AddEdge("a", "d", 0)

	r, err := g.BFS("a")
	if err != nil {
		t.Fatalf("bfs: %v", err)
	}
	if r.Dist["a"] != 0 || r.Dist["b"] != 1 || r.Dist["c"] != 2 || r.Dist["d"] != 1 {
		t.Errorf("wrong dist: %+v", r.Dist)
	}
	if r.Order[0] != "a" {
		t.Errorf("expected a first, got %v", r.Order)
	}
}

func TestBFS_Until(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 0)
	_ = g.AddEdge("b", "c", 0)
	_ = g.AddEdge("c", "d", 0)
	_ = g.AddEdge("a", "e", 0)
	_ = g.AddEdge("e", "f", 0)

	r, err := g.BFSUntil("a", "c")
	if err != nil {
		t.Fatalf("bfs: %v", err)
	}
	if r.Dist["c"] != 2 {
		t.Errorf("expected dist[c]=2, got %d", r.Dist["c"])
	}
	// 找到 c 后立即停，所以 d 不在 dist 中
	if _, ok := r.Dist["d"]; ok {
		t.Error("expected d not visited (early stop)")
	}
	// 路径: a -> b -> c
	expectedPath := []string{"a", "b", "c"}
	if !equalStrings(r.Path, expectedPath) {
		t.Errorf("expected path %v, got %v", expectedPath, r.Path)
	}
}

func TestBFS_NotFound(t *testing.T) {
	g := NewGraph()
	_, err := g.BFS("nonexistent")
	if err != ErrNodeNotFound {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestBFS_Disconnected(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 0)
	_ = g.AddNode("c", "", nil) // 孤立节点
	r, _ := g.BFS("a")
	if _, ok := r.Dist["c"]; ok {
		t.Error("expected c not reachable from a")
	}
}

func TestDijkstra_Simple(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 1)
	_ = g.AddEdge("b", "c", 1)
	_ = g.AddEdge("a", "c", 5) // 慢路径

	r, err := g.Dijkstra("a", "c")
	if err != nil {
		t.Fatalf("dijkstra: %v", err)
	}
	if r.TotalWeight != 2 {
		t.Errorf("expected shortest weight 2, got %f", r.TotalWeight)
	}
	expectedPath := []string{"a", "b", "c"}
	if !equalStrings(r.Path, expectedPath) {
		t.Errorf("expected path %v, got %v", expectedPath, r.Path)
	}
}

func TestDijkstra_AllDistances(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 3)
	_ = g.AddEdge("a", "c", 1)
	_ = g.AddEdge("c", "b", 1) // a->c->b = 2, 比 a->b=3 短

	r, err := g.Dijkstra("a", "")
	if err != nil {
		t.Fatalf("dijkstra: %v", err)
	}
	if r.Dist["a"] != 0 {
		t.Errorf("expected dist[a]=0, got %f", r.Dist["a"])
	}
	if r.Dist["b"] != 2 {
		t.Errorf("expected dist[b]=2 (a->c->b), got %f", r.Dist["b"])
	}
	if r.Dist["c"] != 1 {
		t.Errorf("expected dist[c]=1, got %f", r.Dist["c"])
	}
}

func TestDijkstra_NotFound(t *testing.T) {
	g := NewGraph()
	_, err := g.Dijkstra("nonexistent", "")
	if err != ErrNodeNotFound {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestDijkstra_Disconnected(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 1)
	_ = g.AddNode("c", "", nil)
	r, _ := g.Dijkstra("a", "c")
	if r.TotalWeight != 0 {
		t.Errorf("expected no path (total=0), got %f", r.TotalWeight)
	}
	if r.Path != nil {
		t.Errorf("expected nil path, got %v", r.Path)
	}
}

func TestReconstructPath_NotFound(t *testing.T) {
	g := NewGraph()
	_ = g.AddEdge("a", "b", 0)
	r, _ := g.BFSUntil("a", "z") // z 不存在
	if r.Path != nil && len(r.Path) > 0 {
		t.Errorf("expected no path, got %v", r.Path)
	}
}

func TestDijkstra_DefaultWeight(t *testing.T) {
	// weight=0 应被替换为 1.0（无权图）
	g := NewGraph()
	_ = g.AddEdge("a", "b", 0)
	r, _ := g.Dijkstra("a", "b")
	if r.TotalWeight != 1.0 {
		t.Errorf("expected weight 1.0 (default for 0), got %f", r.TotalWeight)
	}
}

func TestDijkstra_LargeGraph(t *testing.T) {
	// 链式图：1-2-3-...-100
	g := NewGraph()
	for i := 0; i < 99; i++ {
		_ = g.AddEdge(nodeID(i), nodeID(i+1), 1)
	}
	r, _ := g.Dijkstra(nodeID(0), nodeID(99))
	if r.TotalWeight != 99 {
		t.Errorf("expected dist=99, got %f", r.TotalWeight)
	}
}

func nodeID(i int) string {
	return "n" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 防止 unused 警告
var _ = sort.Strings
var _ = math.Abs
