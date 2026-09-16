package api

import (
	"encoding/json"
	"net/http"
)

// TrackingHandler 提供 GET /api/tracking
//
// P1-F 切片 3 mock 返回：项目级元数据（与 Python V1.5.x /api/tracking schema 部分对齐）。
// 完整实现（chapters / characters / foreshadows / word counts）留 P2 阶段接 SQLite。
type TrackingHandler struct{}

// NewTrackingHandler 创建
func NewTrackingHandler() *TrackingHandler { return &TrackingHandler{} }

// ServeHTTP 路由分发
func (h *TrackingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}

	// mock：未初始化项目
	resp := map[string]any{
		"initialized":             false,
		"project_root":            projectRoot,
		"note":                    "P1-F mock; full tracking state requires chapters package (P2)",
		"total_chapters_target":   100,
		"total_word_count_target": 100000,
		"last_updated_chapter":    0,
		"character_count":         0,
		"foreshadow_count":        0,
		"completed_chapter_count": 0,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}
