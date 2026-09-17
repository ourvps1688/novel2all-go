// chroma_handler.go 提供 /api/chroma/* 端点（管理 API）。
//
// 路由：
//
//	POST   /api/chroma/upsert       插入/更新文档
//	POST   /api/chroma/search      top-K 相似搜索
//	GET    /api/chroma/stats       存储统计
//	GET    /api/chroma/{id}        取单个文档
//	DELETE /api/chroma/{id}        删除文档
//	POST   /api/chroma/clear       清空所有文档
//
// 设计：
//   - 无需 admin 鉴权（P1 阶段）：chroma 是 RAG 基础设施，业务侧鉴权
//   - 后续 P2 阶段可加 auth 限制
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/chroma"
)

// ChromaHandler /api/chroma/* handler
type ChromaHandler struct {
	client *chroma.Client
}

// NewChromaHandler 创建
func NewChromaHandler(c *chroma.Client) *ChromaHandler {
	return &ChromaHandler{client: c}
}

// ServeHTTP 路由分发
func (h *ChromaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/chroma")
	path = strings.Trim(path, "/")

	// /api/chroma/{id} → /{id}
	// /api/chroma/upsert → /upsert
	// /api/chroma → ""
	switch path {
	case "":
		http.NotFound(w, r)
	case "upsert":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleUpsert(w, r)
	case "search":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleSearch(w, r)
	case routeStats:
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
	default:
		// /{id} → GET or DELETE
		switch r.Method {
		case http.MethodGet:
			h.handleGet(w, r, path)
		case http.MethodDelete:
			h.handleDelete(w, r, path)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// UpsertRequest POST /api/chroma/upsert
type UpsertRequest struct {
	Documents []chroma.Document `json:"documents"`
}

// handleUpsert POST /api/chroma/upsert
func (h *ChromaHandler) handleUpsert(w http.ResponseWriter, r *http.Request) {
	var req UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	ids := h.client.Upsert(req.Documents)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ids":     ids,
		"count":   len(ids),
		"message": fmtMessage(len(ids)),
	})
}

// SearchRequest POST /api/chroma/search
type SearchRequest struct {
	Text string `json:"text"`
	K    int    `json:"k,omitempty"`
}

// handleSearch POST /api/chroma/search
func (h *ChromaHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	matches := h.client.Query(req.Text, req.K)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"matches": matches,
		"count":   len(matches),
		"query":   req.Text,
	})
}

// handleStats GET /api/chroma/stats
func (h *ChromaHandler) handleStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(h.client.Stats())
}

// handleGet GET /api/chroma/{id}
func (h *ChromaHandler) handleGet(w http.ResponseWriter, _ *http.Request, id string) {
	d, err := h.client.Get(id)
	if err != nil {
		if errors.Is(err, chroma.ErrNotFound) {
			http.Error(w, `{"error":"document not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(d)
}

// handleDelete DELETE /api/chroma/{id}
func (h *ChromaHandler) handleDelete(w http.ResponseWriter, _ *http.Request, id string) {
	if err := h.client.Delete(id); err != nil {
		if errors.Is(err, chroma.ErrNotFound) {
			http.Error(w, `{"error":"document not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleClear POST /api/chroma/clear
func (h *ChromaHandler) handleClear(w http.ResponseWriter, _ *http.Request) {
	h.client.Clear()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"cleared": true,
		"message": "all documents deleted",
	})
}

// fmtMessage format count → human message
func fmtMessage(n int) string {
	if n == 1 {
		return "1 document upserted"
	}
	return strconv.Itoa(n) + " documents upserted"
}
