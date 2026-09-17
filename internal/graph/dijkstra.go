// dijkstra.go - Dijkstra 最短路径算法
//
// 用途：
//   - 加权图的最短路径（人物关系权重 = 亲密程度）
//   - 路径规划（事件依赖链最短化）
//
// 复杂度：O(V² + E) 适合 P1 阶段规模（V < 10K）
// P2 阶段可换 heap-based 实现降到 O((V+E) log V)
package graph

import (
	"math"
	"sort"
)

// DijkstraResult 最短路径结果
type DijkstraResult struct {
	// Dist 节点到 source 的最短距离
	Dist map[string]float64 `json:"dist"`
	// Parent 每个节点的父节点（路径重建用）
	Parent map[string]string `json:"parent,omitempty"`
	// Path source → target 的最短路径（target="" 表示所有节点）
	Path []string `json:"path,omitempty"`
	// TotalWeight 路径总权重
	TotalWeight float64 `json:"total_weight,omitempty"`
}

// Dijkstra 单源最短路径
//
// target == "" 返回所有节点的距离
// target != "" 找到 target 后返回（早停优化）
func (g *Graph) Dijkstra(source, target string) (*DijkstraResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, ok := g.nodes[source]; !ok {
		return nil, ErrNodeNotFound
	}
	if target != "" {
		if _, ok := g.nodes[target]; !ok {
			return nil, ErrNodeNotFound
		}
	}

	dist := map[string]float64{source: 0}
	parent := map[string]string{}
	visited := map[string]bool{}

	// 简单实现：每轮遍历所有未访问节点找最小
	// 适合小图（< 10K 节点），P2 阶段可换 heap
	for {
		cur, ok := g.findMinUnvisited(dist, visited)
		if !ok {
			break // 所有可达节点都访问完
		}
		visited[cur] = true

		if cur == target {
			break
		}

		g.relaxNeighbors(cur, dist, parent, visited)
	}

	return g.buildDijkstraResult(target, dist, parent), nil
}

// findMinUnvisited 找 dist 中未访问的最小距离节点
func (g *Graph) findMinUnvisited(dist map[string]float64, visited map[string]bool) (string, bool) {
	cur := ""
	minDist := math.Inf(1)
	for id, d := range dist {
		if visited[id] {
			continue
		}
		if d < minDist {
			minDist = d
			cur = id
		}
	}
	if cur == "" {
		return "", false
	}
	return cur, true
}

// relaxNeighbors 松弛 cur 的所有未访问邻居
func (g *Graph) relaxNeighbors(cur string, dist map[string]float64, parent map[string]string, visited map[string]bool) {
	neighbors := make([]string, 0, len(g.nodes[cur]))
	for to := range g.nodes[cur] {
		if !visited[to] {
			neighbors = append(neighbors, to)
		}
	}
	sort.Strings(neighbors)
	for _, to := range neighbors {
		w := g.nodes[cur][to]
		newDist := dist[cur] + w
		if old, ok := dist[to]; !ok || newDist < old {
			dist[to] = newDist
			parent[to] = cur
		}
	}
}

// buildDijkstraResult 构造响应
func (g *Graph) buildDijkstraResult(target string, dist map[string]float64, parent map[string]string) *DijkstraResult {
	result := &DijkstraResult{
		Dist:   dist,
		Parent: parent,
	}
	if target != "" {
		if _, ok := dist[target]; ok {
			result.Path = reconstructPath(parent, target)
			result.TotalWeight = dist[target]
		}
	}
	return result
}
