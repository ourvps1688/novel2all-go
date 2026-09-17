package skills

import (
	"context"
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/roles"
)

// ShortModeStages 返回短篇 8 节 stages（按依赖排序）
//
// 8 节 pipeline（基于 novel2all V1.5.5 core/short_mode.py）：
//  1. outline           - 大纲（story_outliner）
//  2. character_setup   - 人物设定（character_extractor）
//  3. chapter_write     - 章节写作（chapter_writer）
//  4. consistency_check - 一致性检查（consistency_checker）
//  5. chapter_review    - 章节审稿（story_reviewer）
//  6. deslop            - 去 AI 味（deslop skill）
//  7. polish            - 润色（chapter_writer 二次）
//  8. final             - 终稿
//
// 7 8 依赖 1 2 3 4 5 6；最终依赖前面所有 stage
func ShortModeStages(storyIdea string) []Stage {
	return []Stage{
		{
			Name:  "outline",
			Skill: "story-short-write", // story-short-write.md (含 outline + write)
			Vars: map[string]string{
				"__user_input__": storyIdea,
			},
		},
		{
			Name:    "character_setup",
			Skill:   "story-short-analyze", // story-short-analyze.md (角色提取)
			Depends: []string{"outline"},
			Vars: map[string]string{
				"__user_input__": "基于大纲提取主要角色信息: {{outline}}",
			},
		},
		{
			Name:    "chapter_write",
			Skill:   "story-short-write",
			Depends: []string{"character_setup"},
			Vars: map[string]string{
				"__user_input__": "基于大纲和角色设定写完整短篇: 大纲={{outline}}, 角色={{character_setup}}",
			},
		},
		{
			Name:    "consistency_check",
			Skill:   "story-short-analyze", // 同 SKILL 分析模式
			Depends: []string{"chapter_write"},
			Vars: map[string]string{
				"__user_input__": "对短篇做一致性检查, 输出 JSON issues: {{chapter_write}}",
			},
		},
		{
			Name:    "chapter_review",
			Skill:   "story-short-analyze",
			Depends: []string{"consistency_check"},
			Vars: map[string]string{
				"__user_input__": "审稿: 短篇={{chapter_write}}, 一致性issues={{consistency_check}}",
			},
		},
		{
			Name:    "deslop",
			Skill:   "story-deslop",
			Depends: []string{"chapter_review"},
			Vars: map[string]string{
				"__user_input__": "去 AI 味: {{chapter_write}}",
			},
		},
		{
			Name:    "polish",
			Skill:   "story-short-write",
			Depends: []string{"deslop"},
			Vars: map[string]string{
				"__user_input__": "润色 (去除 redundant + 优化句式): {{deslop}}",
			},
		},
		{
			Name:    "final",
			Skill:   "story-short-write",
			Depends: []string{"polish"},
			Vars: map[string]string{
				"__user_input__": "最终汇编短篇输出: {{polish}}",
			},
		},
	}
}

// CompileShortStory 跑短篇 8 节 pipeline
//
// 输入: storyIdea + user context (可选)
// 返回: 8 节 results (map[stage_name]Result)
// 失败: stage 错误（错误信息包含 stage name）
func CompileShortStory(ctx context.Context, p *Pipeline, storyIdea string) (map[string]Result, error) {
	if p == nil {
		return nil, fmt.Errorf("pipeline is nil")
	}
	if storyIdea == "" {
		return nil, fmt.Errorf("story idea is required")
	}

	stages := ShortModeStages(storyIdea)
	return p.Run(ctx, stages)
}

// ShortModeStagesForRole 返回负责特定 stage 的 role ID
//
// 例如: ShortModeStagesForRole("outline") 返回 RoleStoryOutliner
func ShortModeStagesForRole(stageName string) (roles.Role, bool) {
	r, ok := roles.StageToRole(stageName)
	if !ok {
		return "", false
	}
	return r.ID, true
}
