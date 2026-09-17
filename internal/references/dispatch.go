// load_dispatch.go: skill → reference 映射 (Sprint 35)
//
// 映射原则:
//   - default refs 所有 skill 都加载 (基础写作 + 平台特征)
//   - skill-specific refs 按 skill 性质加 (写长篇 → 大纲/伏笔; 写短篇 → 钩子/反转)
//
// 新增 reference: 放 .md 文件到 assets/ + 在 defaultSkillDispatch 加 mapping.
package references

// defaultRefs 所有 skill 都加载的基础 references.
//
// 覆盖: 通用写作技巧 + 文风锚定 + 去 AI 味.
func defaultRefs() []string {
	return []string{
		"style-anchor",
		"deslop-rules",
		"platform-style",
	}
}

// defaultSkillDispatch skill → 附加 references.
//
// 设计原则:
//   - story-long-write: 大纲排布 + 角色设计 + 伏笔 + 剧情节奏
//   - story-long-analyze: 拆解维度 + 一致性检查
//   - story-long-scan: 平台趋势 + 题材分布
//   - story-short-write: 钩子技法 + 反转 + 节奏紧凑
//   - story-short-analyze: 情绪设计 + 高潮曲线
//   - story-short-scan: 短篇平台特征
//   - story-review: 视角审查 + 伏笔 + 角色一致性
//   - story-cover: 封面风格 + 文案模板
//   - story-import: 章节提取 + 来源格式
//   - story-deslop: AI 味规则 (额外规则)
func defaultSkillDispatch() map[string][]string {
	return map[string][]string{
		"story-long-write": {
			"outline-structure",
			"character-design",
			"foreshadowing",
			"pacing-techniques",
		},
		"story-long-analyze": {
			"analysis-dimensions",
			"consistency-check",
		},
		"story-long-scan": {
			"genre-trends",
			"platform-style",
		},
		"story-short-write": {
			"hook-techniques",
			"twist-design",
			"short-pacing",
		},
		"story-short-analyze": {
			"emotion-design",
			"climax-curve",
		},
		"story-short-scan": {
			"short-platform-style",
		},
		"story-review": {
			"review-perspectives",
			"foreshadowing",
			"consistency-check",
		},
		"story-cover": {
			"cover-styles",
			"copywriting-templates",
		},
		"story-import": {
			"chapter-extraction",
			"source-formats",
		},
		"story-deslop": {
			"deslop-rules",
			"ai-tells",
		},
		"story-setup": {
			"project-structure",
		},
	}
}
