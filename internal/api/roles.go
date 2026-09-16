package api

import (
	"encoding/json"
	"net/http"
)

// RoleSummary 单个角色
type RoleSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Agent       string `json:"agent,omitempty"`
}

// RolesListResponse GET /api/roles
type RolesListResponse struct {
	Roles []RoleSummary `json:"roles"`
	Count int           `json:"count"`
}

// RolesHandler GET /api/roles
//
// 列出 novel2all 5 个核心角色（来自 V0.30 roles/ 模块）。
// 这是静态元数据，5 个角色恒定。
type RolesHandler struct{}

// NewRolesHandler 创建
func NewRolesHandler() *RolesHandler { return &RolesHandler{} }

// ServeHTTP 实现 http.Handler
//
//	GET /api/roles
func (h *RolesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/roles" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roles := []RoleSummary{
		{
			Name:        "story-explorer",
			Description: "查询角色/伏笔/进度等 tracking state 内的元数据",
		},
		{
			Name:        "story-researcher",
			Description: "查询资料/调研/搜索（外部知识）",
		},
		{
			Name:        "story-reviewer",
			Description: "审稿/质量评估/4 角色多视角审查",
		},
		{
			Name:        "story-planner",
			Description: "大纲/细纲/章节规划",
		},
		{
			Name:        "story-writer",
			Description: "正文写作（WRITING 路由专用）",
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(RolesListResponse{
		Roles: roles,
		Count: len(roles),
	})
}
