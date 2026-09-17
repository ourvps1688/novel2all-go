package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/graph"
)

func newTestGraphHandler(t *testing.T) *GraphHandler {
	t.Helper()
	g := graph.NewGraph()
	// 预置 3 节点 + 2 边
	_ = g.AddNode("alice", "Alice", nil)
	_ = g.AddNode("bob", "Bob", nil)
	_ = g.AddNode("charlie", "Charlie", nil)
	_ = g.AddEdge("alice", "bob", 0)
	_ = g.AddEdge("bob", "charlie", 0)
	return NewGraphHandler(g)
}

func TestGraph_AddNode(t *testing.T) {
	h := newTestGraphHandler(t)

	body := `{"id":"dave","label":"Dave"}`
	req := httptest.NewRequest(http.MethodPost, "/api/graph/node", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGraph_AddNode_Invalid(t *testing.T) {
	h := newTestGraphHandler(t)

	// empty id
	body := `{"id":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/graph/node", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty id, got %d", rr.Code)
	}

	// bad json
	req = httptest.NewRequest(http.MethodPost, "/api/graph/node", bytes.NewBufferString("not json"))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad json, got %d", rr.Code)
	}
}

func TestGraph_AddEdge(t *testing.T) {
	h := newTestGraphHandler(t)

	body := `{"from":"alice","to":"charlie","weight":2.5}`
	req := httptest.NewRequest(http.MethodPost, "/api/graph/edge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGraph_AddEdge_NegativeWeight(t *testing.T) {
	h := newTestGraphHandler(t)

	body := `{"from":"alice","to":"charlie","weight":-1}`
	req := httptest.NewRequest(http.MethodPost, "/api/graph/edge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative weight, got %d", rr.Code)
	}
}

func TestGraph_BFS(t *testing.T) {
	h := newTestGraphHandler(t)

	// BFS from alice (full)
	req := httptest.NewRequest(http.MethodGet, "/api/graph/bfs?start=alice", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var result graph.BFSResult
	_ = json.NewDecoder(rr.Body).Decode(&result)
	if result.Dist["alice"] != 0 || result.Dist["bob"] != 1 || result.Dist["charlie"] != 2 {
		t.Errorf("wrong dist: %+v", result.Dist)
	}
}

func TestGraph_BFS_Until(t *testing.T) {
	h := newTestGraphHandler(t)

	// BFS from alice to charlie (early stop)
	req := httptest.NewRequest(http.MethodGet, "/api/graph/bfs?start=alice&end=charlie", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var result graph.BFSResult
	_ = json.NewDecoder(rr.Body).Decode(&result)
	if result.Dist["charlie"] != 2 {
		t.Errorf("expected dist[charlie]=2, got %d", result.Dist["charlie"])
	}
	if len(result.Path) != 3 {
		t.Errorf("expected path len 3, got %v", result.Path)
	}
}

func TestGraph_BFS_MissingStart(t *testing.T) {
	h := newTestGraphHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/graph/bfs", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing start, got %d", rr.Code)
	}
}

func TestGraph_BFS_NotFound(t *testing.T) {
	h := newTestGraphHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/graph/bfs?start=nonexistent", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for nonexistent start, got %d", rr.Code)
	}
}

func TestGraph_Shortest(t *testing.T) {
	h := newTestGraphHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/graph/shortest?from=alice&to=charlie", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var result graph.DijkstraResult
	_ = json.NewDecoder(rr.Body).Decode(&result)
	if result.TotalWeight != 2 {
		t.Errorf("expected weight 2, got %f", result.TotalWeight)
	}
	if len(result.Path) != 3 {
		t.Errorf("expected path len 3, got %v", result.Path)
	}
}

func TestGraph_Shortest_MissingParams(t *testing.T) {
	h := newTestGraphHandler(t)

	// 缺 to
	req := httptest.NewRequest(http.MethodGet, "/api/graph/shortest?from=alice", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 缺 from
	req = httptest.NewRequest(http.MethodGet, "/api/graph/shortest?to=charlie", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestGraph_Stats(t *testing.T) {
	h := newTestGraphHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/graph/stats", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var stats GraphStats
	_ = json.NewDecoder(rr.Body).Decode(&stats)
	if stats.NodeCount != 3 {
		t.Errorf("expected 3 nodes, got %d", stats.NodeCount)
	}
	if stats.EdgeCount != 2 {
		t.Errorf("expected 2 edges, got %d", stats.EdgeCount)
	}
}

func TestGraph_Nodes_Edges(t *testing.T) {
	h := newTestGraphHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/graph/nodes", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var resp struct {
		Nodes []string `json:"nodes"`
		Count int      `json:"count"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 3 {
		t.Errorf("expected 3 nodes, got %d", resp.Count)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/graph/edges", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 2 {
		t.Errorf("expected 2 edges, got %d", resp.Count)
	}
}

func TestGraph_Clear(t *testing.T) {
	h := newTestGraphHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/graph/clear", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/graph/stats", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var stats GraphStats
	_ = json.NewDecoder(rr.Body).Decode(&stats)
	if stats.NodeCount != 0 || stats.EdgeCount != 0 {
		t.Errorf("expected 0/0 after clear, got %d/%d", stats.NodeCount, stats.EdgeCount)
	}
}

func TestGraph_MethodNotAllowed(t *testing.T) {
	h := newTestGraphHandler(t)

	// GET /api/graph/node → 405
	req := httptest.NewRequest(http.MethodGet, "/api/graph/node", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestGraph_NotFound(t *testing.T) {
	h := newTestGraphHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/graph/unknown", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}
