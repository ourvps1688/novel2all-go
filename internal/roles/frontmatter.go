package roles

import (
	"bufio"
	"fmt"
	"strings"
)

// frontmatter 解析后的字段（仅支持 vendor role 实际用到的子集）
type frontmatter struct {
	Name        string
	Description string
	Tools       []string
	Model       string
	MaxTurns    int
	Memory      string
	Skills      []string
}

// parseYAMLFrontmatter 解析 role .md 文件的 frontmatter + body
//
// 与 agent 包的 Frontmatter parser 等价，但内嵌避免循环依赖。
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

		switch key {
		case "name":
			fm.Name = value
		case "description":
			if value == "|" || value == ">" {
				multiline, consumed := readIndentedBlock(lines, i+1)
				fm.Description = strings.TrimRight(multiline, "\n")
				i += 1 + consumed
				continue
			}
			fm.Description = value
		case "tools", "disallowedTools":
			list, consumed := parseListValue(value, lines, i+1)
			if key == "tools" {
				fm.Tools = list
			} else {
				// disallowedTools 暂不存入 RoleSpec（待 A5 实现）
			}
			i += 1 + consumed
			continue
		case "model":
			fm.Model = value
		case "maxTurns":
			var n int
			if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
				return fm, fmt.Errorf("maxTurns: %w", err)
			}
			fm.MaxTurns = n
		case "memory":
			fm.Memory = value
		case "skills":
			list, consumed := parseListValue(value, lines, i+1)
			fm.Skills = list
			i += 1 + consumed
			continue
		}
		i++
	}
	return fm, nil
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
