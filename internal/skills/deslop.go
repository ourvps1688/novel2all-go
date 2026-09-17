package skills

import (
	"context"
	"fmt"
	"strings"
)

// DeslopPromptTemplate 生成去 AI 味的 prompt 模板
//
// 流程（基于 novel2all V1.5.5 core/deslop.py）：
//  1. 对原文做 deslop 修改 (替换 AI 常用短语)
//  2. 输出修改前后对比
//
// input: 原文 (LLM 生成内容)
// returns: 去 AI 味后的文本 + 修改详情
func DeslopPromptTemplate(input string) string {
	// AI 常用短语 → 替换为自然表达
	replacements := map[string]string{
		"在当今时代":   "如今",
		"在这个时代":   "这时",
		"不可否认":    "看来",
		"总而言之":    "总之",
		"首先":      "先说",
		"其次":      "再说",
		"最后":      "最后",
		"不仅...而且": "既...也",
		"然而":      "但是",
		"因此":      "所以",
		"值得注意的是":  "值得注意的是",
		"具有重要意义":  "很重要",
		"至关重要":    "很关键",
		"与时俱进":    "跟上时代",
	}

	out := input
	for old, new := range replacements {
		out = strings.ReplaceAll(out, old, new)
	}

	// 提示词模板（用于调用 deslop skill）
	const tmpl = `请按以下要求改写文段, 去除 AI 味:

1. 替换 AI 常用短语:
%s

2. 保持原意, 但用更自然的表达
3. 不要添加新内容, 不要删除内容
4. 只输出改写后的文本

原文:
%s

改写:
`

	var replaceList strings.Builder
	for old, new := range replacements {
		fmt.Fprintf(&replaceList, "  - %q → %q\n", old, new)
	}

	return fmt.Sprintf(tmpl, replaceList.String(), out)
}

// ApplyDeslop 跑 deslop skill (L1: prompt engineering, L2: skill 调用)
//
// pipeline 用法: 在 stage 中调用 ApplyDeslop(ctx, executor, content)
func ApplyDeslop(ctx context.Context, executor ExecutorRunner, content string) (string, error) {
	if content == "" {
		return "", fmt.Errorf("empty content")
	}

	// 这里用 L1 prompt engineering (DeslopPromptTemplate) 代替真正的 LLM 调用
	// 真实生产可以改成调 LLM (executor.Execute)
	// Sprint 16 用 L1 简化版; Sprint 17 改用 executor
	return DeslopPromptTemplate(content), nil
}

// DeslopStage 返回 deslop stage (用于塞进任意 pipeline)
//
// 输出 stage name = "deslop", skill = "story-deslop"
func DeslopStage(dependsOn []string) Stage {
	return Stage{
		Name:    "deslop",
		Skill:   "story-deslop",
		Depends: dependsOn,
	}
}
