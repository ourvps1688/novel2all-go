// graph_handler.go 提供 /api/graph/* 端点。
//
// 路由：
//
//	POST /api/graph/node       加节点
//	POST /api/graph/edge       加边
//	GET  /api/graph/bfs        BFS（?start=xxx&end=yyy）
//	GET  /api/graph/shortest   Dijkstra（?from=xxx&to=yyy）
//	GET  /api/graph/stats      统计
//	POST /api/graph/clear      清空
//	GET  /api/graph/nodes      列出所有节点
//	GET  /api/graph/edges      列出所有边
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/graph"
)

// route 名称常量 (goconst: 避免 "stats" 等字面量重复)
const routeStats = "stats"

const (
	routeNode     = "node"
	routeEdge     = "edge"
	routeBFS      = "bfs"
	routeShortest = "shortest"
	routeClear    = "clear"
	routeNodes    = "nodes"
	routeEdges    = "edges"
)

// GraphHandler /api/graph/* handler
type GraphHandler struct {
	g *graph.Graph
}

// NewGraphHandler 创建
func NewGraphHandler(g *graph.Graph) *GraphHandler {
	return &GraphHandler{g: g}
}

// ServeHTTP 路由分发
//
// 用 expectedMethodFor 拆分方法校验逻辑 → gocyclo 降低
func (h *GraphHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/graph")
	path = strings.Trim(path, "/")

	if path == "" {
		http.NotFound(w, r)
		return
	}

	// 校验 HTTP 方法（goconst: 405 而非 404）
	expected := expectedMethodFor(path)
	if expected != "" && r.Method != expected {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch path {
	case routeNode:
		h.handleAddNode(w, r)
	case routeEdge:
		h.handleAddEdge(w, r)
	case routeBFS:
		h.handleBFS(w, r)
	case routeShortest:
		h.handleShortest(w, r)
	case routeStats:
		h.handleStats(w, r)
	case routeClear:
		h.handleClear(w, r)
	case routeNodes:
		h.handleNodes(w, r)
	case routeEdges:
		h.handleEdges(w, r)
	default:
		http.NotFound(w, r)
	}
}

// expectedMethodFor 返回 route 的预期 HTTP 方法（gocyclo 拆分 helper）
func expectedMethodFor(path string) string {
	switch path {
	case routeNode, routeEdge, routeClear:
		return http.MethodPost
	case routeBFS, routeShortest, routeStats, routeNodes, routeEdges:
		return http.MethodGet
	}
	return ""
}

// AddNodeRequest POST /api/graph/node
type AddNodeRequest struct {
	ID    string                 `json:"id"`
	Label string                 `json:"label,omitempty"`
	Attrs map[string]interface{} `json:"attrs,omitempty"`
}

// handleAddNode POST /api/graph/node
func (h *GraphHandler) handleAddNode(w http.ResponseWriter, r *http.Request) {
	var req AddNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if err := h.g.AddNode(req.ID, req.Label, req.Attrs); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"added": true,
		"id":    req.ID,
	})
}

// AddEdgeRequest POST /api/graph/edge
type AddEdgeRequest struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Weight float64 `json:"weight,omitempty"` // 0 = 默认 1.0
}

// handleAddEdge POST /api/graph/edge
func (h *GraphHandler) handleAddEdge(w http.ResponseWriter, r *http.Request) {
	var req AddEdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if err := h.g.AddEdge(req.From, req.To, req.Weight); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"added": true,
		"from":  req.From,
		"to":    req.To,
	})
}

// handleBFS GET /api/graph/bfs?start=xxx&end=yyy
func (h *GraphHandler) handleBFS(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start := q.Get("start")
	end := q.Get("end")
	if start == "" {
		http.Error(w, `{"error":"start query param required"}`, http.StatusBadRequest)
		return
	}
	var (
		result *graph.BFSResult
		err    error
	)
	if end != "" {
		result, err = h.g.BFSUntil(start, end)
	} else {
		result, err = h.g.BFS(start)
	}
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}

// handleShortest GET /api/graph/shortest?from=xxx&to=yyy
func (h *GraphHandler) handleShortest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from := q.Get("from")
	to := q.Get("to")
	if from == "" || to == "" {
		http.Error(w, `{"error":"from and to query params required"}`, http.StatusBadRequest)
		return
	}
	result, err := h.g.Dijkstra(from, to)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}

// GraphStats 统计响应
type GraphStats struct {
	NodeCount int `json:"node_count"`
	EdgeCount int `json:"edge_count"`
}

// handleStats GET /api/graph/stats
func (h *GraphHandler) handleStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(GraphStats{
		NodeCount: h.g.NodeCount(),
		EdgeCount: h.g.EdgeCount(),
	})
}

// handleClear POST /api/graph/clear
func (h *GraphHandler) handleClear(w http.ResponseWriter, _ *http.Request) {
	h.g.Clear()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"cleared": true,
	})
}

// handleNodes GET /api/graph/nodes
func (h *GraphHandler) handleNodes(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"nodes": h.g.Nodes(),
		"count": h.g.NodeCount(),
	})
}

// handleEdges GET /api/graph/edges
func (h *GraphHandler) handleEdges(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"edges": h.g.Edges(),
		"count": h.g.EdgeCount(),
	})
}

// 防止 unused 警告
var _ = strconv.Itoa
