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

// GraphHandler /api/graph/* handler
type GraphHandler struct {
	g *graph.Graph
}

// NewGraphHandler 创建
func NewGraphHandler(g *graph.Graph) *GraphHandler {
	return &GraphHandler{g: g}
}

// ServeHTTP 路由分发
func (h *GraphHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/graph")
	path = strings.Trim(path, "/")

	switch path {
	case "":
		http.NotFound(w, r)
	case "node":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleAddNode(w, r)
	case "edge":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleAddEdge(w, r)
	case "bfs":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleBFS(w, r)
	case "shortest":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleShortest(w, r)
	case "stats":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleStats(w, r)
	case "clear":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleClear(w, r)
	case "nodes":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleNodes(w, r)
	case "edges":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleEdges(w, r)
	default:
		http.NotFound(w, r)
	}
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
