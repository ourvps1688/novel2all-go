// Package roles 提供 5 个核心角色的常量定义。
//
// 5 个角色映射 novel2all Python V1.5.5 core/roles.py 的角色定义（编译时嵌入）。
// 用于：
//   - /api/roles 端点查询
//   - 短篇 8 节 stage runner (skills/short_mode.go) 角色分配
//   - 去 AI 味 skill (skills/deslop.go) 提示
package roles

import (
	"fmt"
	"sort"
	"sync"
)

// Role 角色 ID 枚举
type Role string

const (
	RoleStoryOutliner      Role = "story_outliner"      // 大纲师
	RoleChapterWriter      Role = "chapter_writer"      // 章节作者
	RoleConsistencyChecker Role = "consistency_checker" // 一致性检查
	RoleCharacterExtractor Role = "character_extractor" // 角色提取
	RoleStoryReviewer      Role = "story_reviewer"      // 审稿
)

// RoleMeta 角色元数据（用于 /api/roles API + 文档）
type RoleMeta struct {
	ID          Role   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Agent       string `json:"agent,omitempty"` // 用于 routing 表 (e.g. "consistency")
	Stage       string `json:"stage,omitempty"` // 短篇 8 节中该角色负责的 stage
}

// 5 个角色常量定义（按 P1-F 切片 1 roles.go 同步）
//
// 顺序：写作流程链（outliner → writer → reviewer → consistency → extractor）
var roles = []RoleMeta{
	{
		ID:          RoleStoryOutliner,
		Name:        "故事大纲师",
		Description: "负责构思故事大纲、章节结构、核心情节线",
		Agent:       "outliner",
		Stage:       "outline",
	},
	{
		ID:          RoleChapterWriter,
		Name:        "章节作者",
		Description: "基于大纲撰写具体章节内容，注重文笔、人物对话、场景描写",
		Agent:       "writer",
		Stage:       "chapter_write",
	},
	{
		ID:          RoleStoryReviewer,
		Name:        "故事审稿",
		Description: "对章节进行审阅，标注问题并给出修改建议（错别字/逻辑漏洞/AI 味）",
		Agent:       "reviewer",
		Stage:       "chapter_review",
	},
	{
		ID:          RoleConsistencyChecker,
		Name:        "一致性检查",
		Description: "检查人物性格/时间线/设定的一致性，输出 issues JSON",
		Agent:       "consistency",
		Stage:       "consistency_check",
	},
	{
		ID:          RoleCharacterExtractor,
		Name:        "角色提取",
		Description: "从章节中提取人物卡片（姓名/性格/关系/弧线）",
		Agent:       "extractor",
		Stage:       "character_extract",
	},
}

// All 返回所有 5 个角色（按 ID 字母序）
func All() []RoleMeta {
	out := make([]RoleMeta, len(roles))
	copy(out, roles)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// Get 按 ID 取角色
func Get(id Role) (RoleMeta, error) {
	for _, r := range roles {
		if r.ID == id {
			return r, nil
		}
	}
	return RoleMeta{}, fmt.Errorf("role %q not found", id)
}

// IsValid 检查 role ID 是否存在
func IsValid(id Role) bool {
	for _, r := range roles {
		if r.ID == id {
			return true
		}
	}
	return false
}

// Agents 返回所有 role 的 agent 字段（用于构建 routing 表）
//
// 例: {"story_outliner": "outliner", "chapter_writer": "writer", ...}
func Agents() map[Role]string {
	out := make(map[Role]string, len(roles))
	for _, r := range roles {
		out[r.ID] = r.Agent
	}
	return out
}

// StageToRole 反向索引 stage name → role
func StageToRole(stage string) (RoleMeta, bool) {
	for _, r := range roles {
		if r.Stage == stage {
			return r, true
		}
	}
	return RoleMeta{}, false
}

// 内部缓存（线程安全）
var (
	rolesOnce sync.Once
	rolesByID map[Role]RoleMeta
)

func init() {
	rolesOnce.Do(func() {
		rolesByID = make(map[Role]RoleMeta, len(roles))
		for _, r := range roles {
			rolesByID[r.ID] = r
		}
	})
}
