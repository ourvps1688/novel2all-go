// Package memory 提供 novel2all-go 长记忆系统.
package memory

// graph.go 实现知识图谱 MemoryGraph (Sprint 23).
//
// 设计:
//   - 包装 internal/graph.Graph 提供邻接表存储
//   - 提供业务 API (add_node/edge/get_neighbors/get_path)
//   - to_dict/from_dict 与 GraphData 互转 (持久化用)
//   - shortest_path 用 BFS (V0 简化, V0.22 Python 用 NetworkX Dijkstra)
//
// 参考 Python V0.22 core/memory/graph.py MemoryGraph.
import (
	"encoding/json"
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/graph"
)

// MemoryGraph 业务图谱 (character/timeline/foreshadowing 等).
//
//nolint:revive // matches Python
type MemoryGraph struct {
	data *GraphData
	// adj adjacency list: node_id → []edge index (for BFS/Dijkstra)
	adj map[string][]int
}

// NewMemoryGraph 从 GraphData 创建 (nil data = empty graph).
func NewMemoryGraph(data *GraphData) *MemoryGraph {
	if data == nil {
		data = &GraphData{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	}
	mg := &MemoryGraph{
		data: data,
		adj:  make(map[string][]int),
	}
	mg.rebuildAdj()
	return mg
}

// FromState 从 TrackingState.Graph 加载.
func FromState(state *TrackingState) *MemoryGraph {
	if state.Graph == nil {
		state.Graph = &GraphData{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	}
	return NewMemoryGraph(state.Graph)
}

// rebuildAdj 重建 adjacency 列表 (每次 data 变化时调用).
func (mg *MemoryGraph) rebuildAdj() {
	mg.adj = make(map[string][]int)
	for i, e := range mg.data.Edges {
		mg.adj[e.FromID] = append(mg.adj[e.FromID], i)
		mg.adj[e.ToID] = append(mg.adj[e.ToID], i)
	}
}

// Data 返回底层 GraphData (用于持久化).
func (mg *MemoryGraph) Data() *GraphData {
	return mg.data
}

// AddNode 添加 node (忽略重复).
func (mg *MemoryGraph) AddNode(node GraphNode) {
	if mg.data.NodeExists(node.ID) {
		return
	}
	mg.data.Nodes = append(mg.data.Nodes, node)
}

// AddEdge 添加 edge (忽略重复).
func (mg *MemoryGraph) AddEdge(edge GraphEdge) {
	if mg.data.EdgeExists(edge.FromID, edge.ToID, edge.Type) {
		return
	}
	mg.data.Edges = append(mg.data.Edges, edge)
	mg.rebuildAdj()
}

// RemoveNode 删除 node (同时移除相关 edges).
func (mg *MemoryGraph) RemoveNode(nodeID string) bool {
	if !mg.data.NodeExists(nodeID) {
		return false
	}
	// remove node
	out := mg.data.Nodes[:0]
	for _, n := range mg.data.Nodes {
		if n.ID != nodeID {
			out = append(out, n)
		}
	}
	mg.data.Nodes = out

	// remove related edges
	out2 := mg.data.Edges[:0]
	for _, e := range mg.data.Edges {
		if e.FromID != nodeID && e.ToID != nodeID {
			out2 = append(out2, e)
		}
	}
	mg.data.Edges = out2
	mg.rebuildAdj()
	return true
}

// GetNode 取 node by ID.
func (mg *MemoryGraph) GetNode(id string) *GraphNode {
	for i := range mg.data.Nodes {
		if mg.data.Nodes[i].ID == id {
			return &mg.data.Nodes[i]
		}
	}
	return nil
}

// HasNode 检查 node 是否存在.
func (mg *MemoryGraph) HasNode(id string) bool {
	return mg.GetNode(id) != nil
}

// GetNeighbors 取 node 的所有邻居 edges (edgeType 过滤可选).
func (mg *MemoryGraph) GetNeighbors(nodeID string, edgeType ...EdgeType) []GraphEdge {
	indices := mg.adj[nodeID]
	out := make([]GraphEdge, 0, len(indices))
	for _, i := range indices {
		e := mg.data.Edges[i]
		if len(edgeType) > 0 {
			match := false
			for _, t := range edgeType {
				if e.Type == t {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// GetEdgesBetween (from, to) 之间的所有 edges.
func (mg *MemoryGraph) GetEdgesBetween(fromID, toID string) []GraphEdge {
	out := []GraphEdge{}
	for _, e := range mg.data.Edges {
		if (e.FromID == fromID && e.ToID == toID) || (e.FromID == toID && e.ToID == fromID) {
			out = append(out, e)
		}
	}
	return out
}

// GetPath BFS 找 from → to 最短路径 (max_hops 限制, 0=不限).
//
// 返回 edges 序列 (path 上每条边). 用 internal/graph.BFS 实现.
func (mg *MemoryGraph) GetPath(fromID, toID string, maxHops int) []GraphEdge {
	if !mg.HasNode(fromID) || !mg.HasNode(toID) {
		return nil
	}

	// 构造 internal/graph.Graph 临时实例
	tmpGraph := graph.NewGraph()
	// nodes (id, label, attrs)
	for _, n := range mg.data.Nodes {
		_ = tmpGraph.AddNode(n.ID, n.Name, nil)
	}
	// edges (无权, weight=1)
	for _, e := range mg.data.Edges {
		_ = tmpGraph.AddEdge(e.FromID, e.ToID, 1)
	}

	// BFSUntil 找 from → to 路径 (返回 BFSResult 含 Path)
	result, err := tmpGraph.BFSUntil(fromID, toID)
	if err != nil || result.Path == nil || len(result.Path) < 2 {
		return nil
	}
	path := result.Path

	// max_hops 限制
	if maxHops > 0 && len(path)-1 > maxHops {
		return nil
	}

	// 把 node 路径转 edge 序列
	edges := make([]GraphEdge, 0, len(path)-1)
	for i := 0; i < len(path)-1; i++ {
		between := mg.GetEdgesBetween(path[i], path[i+1])
		if len(between) > 0 {
			edges = append(edges, between[0])
		}
	}
	return edges
}

// IterNodes 遍历所有 nodes.
func (mg *MemoryGraph) IterNodes() []GraphNode {
	out := make([]GraphNode, len(mg.data.Nodes))
	copy(out, mg.data.Nodes)
	return out
}

// IterEdges 遍历所有 edges.
func (mg *MemoryGraph) IterEdges() []GraphEdge {
	out := make([]GraphEdge, len(mg.data.Edges))
	copy(out, mg.data.Edges)
	return out
}

// Count 返回 (nodes, edges) 数.
func (mg *MemoryGraph) Count() (int, int) {
	return len(mg.data.Nodes), len(mg.data.Edges)
}

// NodeCount 返回节点数 (Sprint 30 helper).
func (mg *MemoryGraph) NodeCount() int {
	return len(mg.data.Nodes)
}

// EdgeCount 返回边数 (Sprint 30 helper).
func (mg *MemoryGraph) EdgeCount() int {
	return len(mg.data.Edges)
}

// Neighbors 返回 node 的所有邻居 ID 列表 (按邻接边顺序).
func (mg *MemoryGraph) Neighbors(nodeID string) []string {
	indices := mg.adj[nodeID]
	out := make([]string, 0, len(indices))
	seen := make(map[string]bool)
	for _, i := range indices {
		e := mg.data.Edges[i]
		var nbr string
		if e.FromID == nodeID {
			nbr = e.ToID
		} else {
			nbr = e.FromID
		}
		if !seen[nbr] {
			seen[nbr] = true
			out = append(out, nbr)
		}
	}
	return out
}

// Subgraph 提取子图 (只含指定 nodes + 它们之间的 edges).
func (mg *MemoryGraph) Subgraph(nodeIDs []string) *MemoryGraph {
	keep := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		keep[id] = true
	}
	out := &GraphData{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	for _, n := range mg.data.Nodes {
		if keep[n.ID] {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range mg.data.Edges {
		if keep[e.FromID] && keep[e.ToID] {
			out.Edges = append(out.Edges, e)
		}
	}
	return NewMemoryGraph(out)
}

// MergeGraph 合并另一个 graph 的 nodes 和 edges (去重 ID).
//
// 修改 mg 的 data, 重建 adj. 返回新增的 nodes/edges 数.
func (mg *MemoryGraph) MergeGraph(other *MemoryGraph) (newNodes, newEdges int) {
	if other == nil {
		return 0, 0
	}
	for _, n := range other.data.Nodes {
		if !mg.data.NodeExists(n.ID) {
			mg.data.Nodes = append(mg.data.Nodes, n)
			newNodes++
		}
	}
	for _, e := range other.data.Edges {
		if !mg.data.EdgeExists(e.FromID, e.ToID, e.Type) {
			mg.data.Edges = append(mg.data.Edges, e)
			newEdges++
		}
	}
	mg.rebuildAdj()
	return
}

// BFSPaths 枚举 from → to 所有最短路径 (BFS, max_depth 限制).
//
// 返回每条路径的 node ID 序列 (含起终点).
func (mg *MemoryGraph) BFSPaths(fromID, toID string, maxDepth int) [][]string {
	if !mg.HasNode(fromID) || !mg.HasNode(toID) {
		return nil
	}
	if fromID == toID {
		return [][]string{{fromID}}
	}

	type frame struct {
		path  []string
		depth int
	}
	var results [][]string
	queue := []frame{{path: []string{fromID}, depth: 0}}
	visited := make(map[string]int) // node → 第一次到达时的深度 (避免环)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if maxDepth > 0 && cur.depth > maxDepth {
			continue
		}

		last := cur.path[len(cur.path)-1]
		if last == toID {
			// 找到一条路径
			p := make([]string, len(cur.path))
			copy(p, cur.path)
			results = append(results, p)
			continue
		}

		if minD, ok := visited[last]; ok && minD < cur.depth {
			continue // 已经以更短深度访问过
		}
		visited[last] = cur.depth

		for _, nbr := range mg.Neighbors(last) {
			// 避免在路径中重复
			visited2 := false
			for _, p := range cur.path {
				if p == nbr {
					visited2 = true
					break
				}
			}
			if visited2 {
				continue
			}
			newPath := make([]string, len(cur.path)+1)
			copy(newPath, cur.path)
			newPath[len(cur.path)] = nbr
			queue = append(queue, frame{path: newPath, depth: cur.depth + 1})
		}
	}
	return results
}

// ToDict 序列化为 dict (持久化用).
func (mg *MemoryGraph) ToDict() map[string]any {
	return map[string]any{
		"nodes": mg.data.Nodes,
		"edges": mg.data.Edges,
	}
}

// ToJSON 序列化为 JSON string.
func (mg *MemoryGraph) ToJSON() (string, error) {
	data, err := json.Marshal(mg.data)
	if err != nil {
		return "", fmt.Errorf("marshal graph: %w", err)
	}
	return string(data), nil
}

// FromDict 从 dict 反序列化.
func FromDict(data map[string]any) (*MemoryGraph, error) {
	nodes, _ := data["nodes"].([]GraphNode)
	edges, _ := data["edges"].([]GraphEdge)
	return NewMemoryGraph(&GraphData{Nodes: nodes, Edges: edges}), nil
}

// FromJSON 从 JSON 反序列化.
func FromJSON(jsonStr string) (*MemoryGraph, error) {
	var data GraphData
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("unmarshal graph: %w", err)
	}
	return NewMemoryGraph(&data), nil
}
