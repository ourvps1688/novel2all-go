// bfs.go - BFS 广度优先搜索
//
// BFS 用途：
//   - 最短无权路径（无权图 BFS 第一条到达即最短）
//   - 层次遍历（找所有在 N 跳内的节点）
//   - 连通分量检测
package graph

import "sort"

// BFSResult BFS 结果
type BFSResult struct {
	// Order 访问顺序（按 BFS 队列出队顺序）
	Order []string `json:"order"`
	// Dist 节点到 start 的最短距离（无权图 BFS 步数）
	Dist map[string]int `json:"dist"`
	// Parent 每个节点的父节点（用于重建路径）
	Parent map[string]string `json:"parent,omitempty"`
	// Path start → end 的最短路径（nil 表示不可达）
	Path []string `json:"path,omitempty"`
}

// BFS 广度优先搜索
func (g *Graph) BFS(start string) (*BFSResult, error) {
	return g.BFSUntil(start, "")
}

// BFSUntil BFS 到 end 停止（end="" 表示遍历全图）
//
// 如果 end == ""，返回所有可达节点 + 顺序
// 如果 end != ""，找到 end 后立即返回（早停优化）
func (g *Graph) BFSUntil(start, end string) (*BFSResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, ok := g.nodes[start]; !ok {
		return nil, ErrNodeNotFound
	}

	visited := map[string]bool{start: true}
	dist := map[string]int{start: 0}
	parent := map[string]string{}
	order := []string{}
	queue := []string{start}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)

		// 找到 end 后立即停止，不再访问 cur 的邻居
		if end != "" && cur == end {
			break
		}

		// 邻居按字典序遍历（确定性）
		neighbors := make([]string, 0, len(g.nodes[cur]))
		for to := range g.nodes[cur] {
			neighbors = append(neighbors, to)
		}
		sort.Strings(neighbors)

		for _, to := range neighbors {
			if visited[to] {
				continue
			}
			visited[to] = true
			dist[to] = dist[cur] + 1
			parent[to] = cur
			queue = append(queue, to)
		}
	}

	result := &BFSResult{
		Order:  order,
		Dist:   dist,
		Parent: parent,
	}
	if end != "" && dist[end] > 0 {
		result.Path = reconstructPath(parent, end)
	}
	return result, nil
}

// reconstructPath 重建 parent 链到 end
func reconstructPath(parent map[string]string, end string) []string {
	path := []string{}
	cur := end
	for {
		path = append([]string{cur}, path...)
		p, ok := parent[cur]
		if !ok {
			break
		}
		cur = p
	}
	return path
}
