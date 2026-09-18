package skills

import (
	"regexp"
	"strings"
)

// skillsLineRe 匹配 `skills: [a, b, c]` 或 `skills: [a]` 行
//
// vendor role .md body 含 `skills: [story-deslop, story-setup]` 等，
// 这里简单 regex 提取（不解析完整 markdown）。
var skillsLineRe = regexp.MustCompile(`(?m)^skills:\s*\[([^\]]+)\]`)

// extractSkillsFromBody 从 SKILL.md body 提取 `skills:` 字段引用的子 skill 列表
func extractSkillsFromBody(body string) []string {
	matches := skillsLineRe.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	var out []string
	for _, m := range matches {
		for _, s := range splitCSV(m[1]) {
			s = strings.TrimSpace(s)
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// splitCSV 简单 split + trim（避免引入 encoding/csv 依赖）
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
