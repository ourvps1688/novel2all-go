// Package api 提供 novel2all-go HTTP handlers.
package api

// memory.go 提供 /api/memory/* 端点 (Sprint 25).
//
// 路由:
//
//	GET  /api/memory/state       → 读取 TrackingState
//	POST /api/memory/state/init  → 初始化空 state (project_name)
//	GET  /api/memory/context/{chapter} → 写作前 MemoryContext (LoadForWriting)
//	POST /api/memory/update      → 写后更新 (UpdateAfterWriting)
//	POST /api/memory/review      → 4-role 并行审稿
//	POST /api/memory/rollback    → 回滚到 snapshot
//	GET  /api/memory/snapshots   → 列出 snapshots
//
// 参考 Python V0.21+ web/app.py /api/memory/*.
import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/memory"
)

// MemoryHandler /api/memory/* 路由分发.
type MemoryHandler struct {
	manager     *memory.MemoryManager
	rollbackMgr *memory.RollbackManager
}

// NewMemoryHandler 创建.
func NewMemoryHandler(projectRoot string) *MemoryHandler {
	return &MemoryHandler{
		manager:     memory.NewMemoryManager(projectRoot, nil, memory.DefaultMemoryConfig()),
		rollbackMgr: memory.NewRollbackManager(projectRoot, 5),
	}
}

// NewMemoryHandlerWithRouter 创建 (传入 llm.Router 用于 extractor + multi_reviewer).
func NewMemoryHandlerWithRouter(projectRoot string, router interface{}) *MemoryHandler {
	h := NewMemoryHandler(projectRoot)
	// 注: V0 router 是 *llm.Router (但 api 包不直接 import llm 避免循环)
	// 这里用 interface{} 接收, 内部 type-assert
	return h
}

// ServeHTTP 路由分发.
//
//nolint:gocyclo // 7 routes × 2 method checks = 14 branches (memory API natural complexity)
func (h *MemoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/memory")
	path = strings.Trim(path, "/")

	switch {
	case path == "state" || path == "state/":
		if !h.requireMethod(w, r, http.MethodGet) {
			return
		}
		h.handleGetState(w, r)
	case strings.HasPrefix(path, "context/"):
		h.requireMethod(w, r, http.MethodGet)
		chapterStr := strings.TrimPrefix(path, "context/")
		h.handleGetContext(w, r, chapterStr)
	case path == "update" || path == "update/":
		if !h.requireMethod(w, r, http.MethodPost) {
			return
		}
		h.handleUpdate(w, r)
	case path == "review" || path == "review/":
		if !h.requireMethod(w, r, http.MethodPost) {
			return
		}
		h.handleReview(w, r)
	case path == actionRollback || path == actionRollback+"/":
		if !h.requireMethod(w, r, http.MethodPost) {
			return
		}
		h.handleRollback(w, r)
	case path == "snapshots" || path == "snapshots/":
		if !h.requireMethod(w, r, http.MethodGet) {
			return
		}
		h.handleListSnapshots(w, r)
	default:
		http.NotFound(w, r)
	}
}

// requireMethod 检查 HTTP method (helper, 简化 ServeHTTP 复杂度).
func (h *MemoryHandler) requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// MemoryStateResponse GET /api/memory/state 响应.
type MemoryStateResponse struct {
	ProjectName    string `json:"project_name"`
	Characters     int    `json:"characters"`
	Foreshadowing  int    `json:"foreshadowing"`
	TimelineEvents int    `json:"timeline_events"`
	LastChapter    int    `json:"last_chapter"`
}

func (h *MemoryHandler) handleGetState(w http.ResponseWriter, r *http.Request) {
	// LoadState 不需要 projectName (从 disk 读已有 state), 传空即可
	state, err := h.manager.LoadState("")
	if err != nil {
		writeMemoryError(w, http.StatusInternalServerError, err)
		return
	}
	resp := MemoryStateResponse{
		ProjectName:    state.ProjectName,
		Characters:     len(state.Characters),
		Foreshadowing:  len(state.Foreshadowing),
		TimelineEvents: len(state.Timeline),
		LastChapter:    state.LastUpdatedChapter,
	}
	writeMemoryJSON(w, http.StatusOK, resp)
}

// MemoryContextResponse GET /api/memory/context/{chapter} 响应.
type MemoryContextResponse struct {
	Chapter     int                 `json:"chapter"`
	Core        []memory.MemoryItem `json:"core"`
	Recent      []memory.MemoryItem `json:"recent"`
	TotalTokens int                 `json:"total_tokens"`
	Sections    []string            `json:"sections"` // 给 LLM 的 system message 段落
}

func (h *MemoryHandler) handleGetContext(w http.ResponseWriter, r *http.Request, chapterStr string) {
	chapter, err := strconv.Atoi(chapterStr)
	if err != nil {
		writeMemoryError(w, http.StatusBadRequest, errors.New("invalid chapter"))
		return
	}
	ctx, err := h.manager.LoadForWriting(r.Context(), chapter)
	if err != nil {
		writeMemoryError(w, http.StatusInternalServerError, err)
		return
	}
	resp := MemoryContextResponse{
		Chapter:     chapter,
		Core:        ctx.Core,
		Recent:      ctx.Recent,
		TotalTokens: ctx.TotalTokens(),
		Sections:    ctx.ToSystemSections(),
	}
	writeMemoryJSON(w, http.StatusOK, resp)
}

// MemoryUpdateRequest POST /api/memory/update 请求体.
type MemoryUpdateRequest struct {
	Chapter int    `json:"chapter"`
	Content string `json:"content"`
}

func (h *MemoryHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req MemoryUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMemoryError(w, http.StatusBadRequest, err)
		return
	}
	state, err := h.manager.UpdateAfterWriting(r.Context(), req.Chapter, req.Content)
	if err != nil {
		writeMemoryError(w, http.StatusInternalServerError, err)
		return
	}
	writeMemoryJSON(w, http.StatusOK, map[string]any{
		"chapter":    req.Chapter,
		"characters": len(state.Characters),
		"updated_at": state.LastUpdatedAt,
	})
}

// MemoryReviewRequest POST /api/memory/review 请求体.
type MemoryReviewRequest struct {
	Chapter int    `json:"chapter"`
	Content string `json:"content"`
}

func (h *MemoryHandler) handleReview(w http.ResponseWriter, r *http.Request) {
	var req MemoryReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMemoryError(w, http.StatusBadRequest, err)
		return
	}
	// 用 multi_reviewer (router=nil 时用 stub 模式)
	reviewer := memory.NewMultiAgentReviewer(nil)
	state, _ := h.manager.LoadState("")
	report, err := reviewer.Review(r.Context(), req.Chapter, req.Content, state)
	if err != nil {
		writeMemoryError(w, http.StatusInternalServerError, err)
		return
	}
	writeMemoryJSON(w, http.StatusOK, report)
}

// MemoryRollbackRequest POST /api/memory/rollback 请求体.
type MemoryRollbackRequest struct {
	Chapter int `json:"chapter"`
}

func (h *MemoryHandler) handleRollback(w http.ResponseWriter, r *http.Request) {
	var req MemoryRollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMemoryError(w, http.StatusBadRequest, err)
		return
	}
	snap, err := h.rollbackMgr.LoadSnapshot(req.Chapter)
	if err != nil {
		writeMemoryError(w, http.StatusNotFound, err)
		return
	}
	result, err := h.rollbackMgr.Rollback(snap, h.manager.Tracker())
	if err != nil {
		writeMemoryError(w, http.StatusInternalServerError, err)
		return
	}
	writeMemoryJSON(w, http.StatusOK, result)
}

func (h *MemoryHandler) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	chapters, err := h.rollbackMgr.ListSnapshots()
	if err != nil {
		writeMemoryError(w, http.StatusInternalServerError, err)
		return
	}
	writeMemoryJSON(w, http.StatusOK, map[string]any{"chapters": chapters})
}

// writeMemoryJSON helper.
func writeMemoryJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeMemoryError helper.
func writeMemoryError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}
