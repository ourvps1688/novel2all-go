package roles

import (
	"bufio"
	"fmt"
	"strings"
)

// frontmatter 解析后的字段（仅支持 vendor role 实际用到的子集）
// Sprint A5.13: DisallowedTools 字段已支持（vendor 4 个只读 role 使用）
type frontmatter struct {
	Name             string
	Description      string
	Tools            []string
	DisallowedTools  []string
	Model            string
	MaxTurns         int
	Memory           string
	Skills           []string
}

// parseYAMLFrontmatter 解析 role .md 文件的 frontmatter + body
//
// 与 agent 包 Frontmatter parser 等价，但内嵌避免循环依赖。
// 支持：字符串 / 多行 description (|) / inline 列表 [a, b] / multi-line 列表 / 整数 / 注释
func parseYAMLFrontmatter(content string) (frontmatter, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return frontmatter{}, content, fmt.Errorf("frontmatter: file must start with ---")
	}

	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx == -1 {
		return frontmatter{}, content, fmt.Errorf("frontmatter: no closing --- found")
	}

	fmLines := lines[1:endIdx]
	body := strings.Join(lines[endIdx+1:], "\n")

	fm, err := parseFrontmatterFields(fmLines)
	if err != nil {
		return frontmatter{}, content, fmt.Errorf("frontmatter: %w", err)
	}
	return fm, body, nil
}

// parseFrontmatterFields 解析 frontmatter 字段列表（拆降低 parseYAMLFrontmatter 圈复杂度）
func parseFrontmatterFields(lines []string) (frontmatter, error) {
	var fm frontmatter
	i := 0
	for i < len(lines) {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		colonIdx := strings.Index(raw, ":")
		if colonIdx <= 0 {
			return fm, fmt.Errorf("invalid line %d: %q", i+1, raw)
		}
		key := strings.TrimSpace(raw[:colonIdx])
		value := strings.TrimSpace(raw[colonIdx+1:])

		next, err := applyFrontmatterField(&fm, key, value, lines, i)
		if err != nil {
			return fm, err
		}
		i = next
	}
	return fm, nil
}

// applyFrontmatterField 应用单个字段，返回下一行索引
func applyFrontmatterField(fm *frontmatter, key, value string, lines []string, i int) (int, error) {
	switch key {
	case "name":
		fm.Name = value
		return i + 1, nil
	case "description":
		return applyDescriptionField(fm, value, lines, i)
	case "tools":
		list, consumed := parseListValue(value, lines, i+1)
		fm.Tools = list
		return i + 1 + consumed, nil
	case "disallowedTools":
		list, consumed := parseListValue(value, lines, i+1)
		fm.DisallowedTools = list
		return i + 1 + consumed, nil
	case "model":
		fm.Model = value
		return i + 1, nil
	case "maxTurns":
		var n int
		if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
			return i + 1, fmt.Errorf("maxTurns: %w", err)
		}
		fm.MaxTurns = n
		return i + 1, nil
	case "memory":
		fm.Memory = value
		return i + 1, nil
	case "skills":
		list, consumed := parseListValue(value, lines, i+1)
		fm.Skills = list
		return i + 1 + consumed, nil
	}
	// 未知 key 跳过
	return i + 1, nil
}

// applyDescriptionField 处理 description（含多行 |）
func applyDescriptionField(fm *frontmatter, value string, lines []string, i int) (int, error) {
	if value == "|" || value == ">" {
		multiline, consumed := readIndentedBlock(lines, i+1)
		fm.Description = strings.TrimRight(multiline, "\n")
		return i + 1 + consumed, nil
	}
	fm.Description = value
	return i + 1, nil
}

func readIndentedBlock(lines []string, start int) (string, int) {
	var sb strings.Builder
	consumed := 0
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			sb.WriteByte('\n')
			consumed++
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			break
		}
		trimmed := strings.TrimLeft(line, " \t")
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(trimmed)
		consumed++
	}
	return sb.String(), consumed
}

func parseListValue(firstValue string, lines []string, start int) ([]string, int) {
	if strings.HasPrefix(firstValue, "[") {
		end := strings.Index(firstValue, "]")
		if end == -1 {
			end = len(firstValue)
		}
		inner := firstValue[1:end]
		parts := strings.Split(inner, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			p = strings.Trim(p, `"'`)
			if p != "" {
				out = append(out, p)
			}
		}
		return out, 0
	}

	var out []string
	consumed := 0
	if firstValue != "" && !strings.HasPrefix(firstValue, "-") {
		out = append(out, strings.Trim(firstValue, `"'`))
	}
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			consumed++
			continue
		}
		if strings.HasPrefix(line, "- ") {
			val := strings.TrimSpace(line[2:])
			val = strings.Trim(val, `"'`)
			if val != "" {
				out = append(out, val)
			}
			consumed++
			continue
		}
		break
	}
	return out, consumed
}
