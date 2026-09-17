// Package graph 提供无向图算法（替代 Python NetworkX）。
//
// 设计目标：
//   - 纯 Go 实现，无外部依赖
//   - thread-safe（sync.RWMutex）
//   - 邻接表存储（节点和边都 map 化）
//   - 支持 BFS、DFS、Dijkstra 最短路径
//
// P1 阶段用于：
//   - 人物关系图谱（characters 之间的关系）
//   - 事件依赖图（chapter → previous_chapter 关系）
//
// 性能：节点数 < 10K 时亚毫秒级，足够 P1 阶段使用
// 后续 P2 可加持久化（BoltDB）或 graphblas 加速
package graph

import (
	"errors"
	"sort"
	"sync"
)

// ErrNodeNotFound 节点不存在
var ErrNodeNotFound = errors.New("graph: node not found")

// ErrEdgeNotFound 边不存在
var ErrEdgeNotFound = errors.New("graph: edge not found")

// Node 图节点
type Node struct {
	ID    string                 `json:"id"`
	Label string                 `json:"label,omitempty"`
	Attrs map[string]interface{} `json:"attrs,omitempty"`
}

// Edge 图边
type Edge struct {
	From   string                 `json:"from"`
	To     string                 `json:"to"`
	Weight float64                `json:"weight"` // 边权重（Dijkstra 用）
	Attrs  map[string]interface{} `json:"attrs,omitempty"`
}

// Graph 无向图
type Graph struct {
	mu    sync.RWMutex
	nodes map[string]map[string]float64 // adjacency list: from -> to -> weight
	meta  map[string]Node               // node metadata
}

// NewGraph 创建空图
func NewGraph() *Graph {
	return &Graph{
		nodes: make(map[string]map[string]float64),
		meta:  make(map[string]Node),
	}
}

// AddNode 加节点
func (g *Graph) AddNode(id, label string, attrs map[string]interface{}) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if id == "" {
		return errors.New("graph: empty node id")
	}
	if _, exists := g.nodes[id]; exists {
		return nil // 已存在：no-op (upsert 语义)
	}
	g.nodes[id] = make(map[string]float64)
	g.meta[id] = Node{ID: id, Label: label, Attrs: attrs}
	return nil
}

// AddEdge 加边
//
// weight 默认 1.0（无权图）
func (g *Graph) AddEdge(from, to string, weight float64) error {
	if from == "" || to == "" {
		return errors.New("graph: empty edge endpoints")
	}
	if weight < 0 {
		return errors.New("graph: negative weight not allowed (use AddDirectedEdge for directed)")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// 自动加节点
	if _, ok := g.nodes[from]; !ok {
		g.nodes[from] = make(map[string]float64)
		g.meta[from] = Node{ID: from}
	}
	if _, ok := g.nodes[to]; !ok {
		g.nodes[to] = make(map[string]float64)
		g.meta[to] = Node{ID: to}
	}
	// 无向图：双向
	if weight == 0 {
		weight = 1.0
	}
	g.nodes[from][to] = weight
	g.nodes[to][from] = weight
	return nil
}

// HasNode 节点存在
func (g *Graph) HasNode(id string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.nodes[id]
	return ok
}

// HasEdge 边存在
func (g *Graph) HasEdge(from, to string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	adj, ok := g.nodes[from]
	if !ok {
		return false
	}
	_, ok = adj[to]
	return ok
}

// Neighbors 节点的邻居
func (g *Graph) Neighbors(id string) ([]string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	adj, ok := g.nodes[id]
	if !ok {
		return nil, ErrNodeNotFound
	}
	out := make([]string, 0, len(adj))
	for to := range adj {
		out = append(out, to)
	}
	sort.Strings(out)
	return out, nil
}

// NodeCount 节点数
func (g *Graph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// EdgeCount 边数
func (g *Graph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	count := 0
	for _, adj := range g.nodes {
		count += len(adj)
	}
	return count / 2 // 无向图：双向存储
}

// Clear 清空图
func (g *Graph) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes = make(map[string]map[string]float64)
	g.meta = make(map[string]Node)
}

// Nodes 返回所有节点 ID（按字典序）
func (g *Graph) Nodes() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Edges 返回所有边
func (g *Graph) Edges() []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	seen := make(map[string]bool)
	out := make([]Edge, 0)
	for from, adj := range g.nodes {
		for to, weight := range adj {
			key := from + "|" + to
			rkey := to + "|" + from
			if seen[key] || seen[rkey] {
				continue
			}
			seen[key] = true
			out = append(out, Edge{From: from, To: to, Weight: weight})
		}
	}
	return out
}
