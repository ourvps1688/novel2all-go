package agent

import (
	"bufio"
	"fmt"
	"strings"
)

// Frontmatter 解析后的 frontmatter 字段（仅支持 vendor role 实际用到的字段）
type Frontmatter struct {
	Name        string
	Description string
	Tools       []string
	Model       string
	MaxTurns    int
	Memory      string
	Skills      []string
}

// ParseFrontmatter 从 markdown 文件内容里提取 --- 之间的 YAML frontmatter
//
// vendor role 实际用到的 YAML 子集：
//   - 字符串（单行）
//   - 多行字符串（description: | 后跟缩进内容）
//   - 列表（[Read, Glob] 或多行 - Read\n - Glob）
//   - 整数（maxTurns: 30）
//   - 注释（# 开头的行）
//
// 不支持嵌套对象 / 锚点 / 复杂类型。vendor 7 role 都不需要。
//
// 返回：解析后的 Frontmatter + 剩余 body 起始位置 + error
func ParseFrontmatter(content string) (Frontmatter, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Frontmatter{}, content, fmt.Errorf("frontmatter: file must start with ---")
	}

	// 找第二个 ---
	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx == -1 {
		return Frontmatter{}, content, fmt.Errorf("frontmatter: no closing --- found")
	}

	fmLines := lines[1:endIdx]
	body := strings.Join(lines[endIdx+1:], "\n")

	fm, err := parseYAMLFields(fmLines)
	if err != nil {
		return Frontmatter{}, content, fmt.Errorf("frontmatter: %w", err)
	}
	return fm, body, nil
}

// parseYAMLFields 解析 frontmatter 内的字段（minimal YAML 子集）
func parseYAMLFields(lines []string) (Frontmatter, error) {
	var fm Frontmatter
	i := 0
	for i < len(lines) {
		raw := lines[i]
		// 跳过空行 + 注释
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		// 必须是 "key: value" 格式
		colonIdx := strings.Index(raw, ":")
		if colonIdx <= 0 {
			return fm, fmt.Errorf("invalid line %d: %q (expected key: value)", i+1, raw)
		}
		key := strings.TrimSpace(raw[:colonIdx])
		value := strings.TrimSpace(raw[colonIdx+1:])

		switch key {
		case "name":
			fm.Name = value
		case "description":
			// 多行字符串：value 是 "|" 或 ">"，后面跟缩进内容
			if value == "|" || value == ">" {
				multiline, consumed := readIndentedBlock(lines, i+1)
				fm.Description = strings.TrimRight(multiline, "\n")
				i += 1 + consumed // 1 是当前 key: | 行
				continue
			}
			fm.Description = value
		case "tools":
			// 列表：[Read, Glob] 或多行 - Read
			list, consumed := parseListValue(value, lines, i+1)
			fm.Tools = list
			i += 1 + consumed // 1 是当前 key: 行
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
			// 同 tools
			list, consumed := parseListValue(value, lines, i+1)
			fm.Skills = list
			i += 1 + consumed // 1 是当前 key: 行
			continue
		default:
			// 未知 key 跳过（保持前向兼容）
		}
		i++
	}
	return fm, nil
}

// readIndentedBlock 读取缩进的多行字符串（description: | 后用）
// 返回：拼接后的字符串 + 消耗的行数
func readIndentedBlock(lines []string, start int) (string, int) {
	var sb strings.Builder
	consumed := 0
	for i := start; i < len(lines); i++ {
		line := lines[i]
		// 缩进行：第一个非空字符是空格或 tab
		if line == "" {
			sb.WriteByte('\n')
			consumed++
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			break // 退出缩进块
		}
		// 去掉前导缩进（2 空格或 1 tab）
		trimmed := strings.TrimLeft(line, " \t")
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(trimmed)
		consumed++
	}
	return sb.String(), consumed
}

// parseListValue 解析列表值（inline [a, b] 或 multi-line - a）
// 返回：列表 + 消耗的行数
func parseListValue(firstValue string, lines []string, start int) ([]string, int) {
	// inline 形式：[Read, Glob, Grep]
	if strings.HasPrefix(firstValue, "[") {
		// 找 ]
		end := strings.Index(firstValue, "]")
		if end == -1 {
			// 跨行（罕见）；简化处理
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

	// multi-line 形式：
	// tools:
	//   - Read
	//   - Glob
	// 或首行是空 + 后续行 - xxx
	var out []string
	consumed := 0

	// 检查首行是否有内容（如 tools: Read 形式，简化为单元素）
	if firstValue != "" && !strings.HasPrefix(firstValue, "-") {
		// 单值形式
		out = append(out, strings.Trim(firstValue, `"'`))
	}

	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			consumed++
			continue
		}
		// - xxx 形式
		if strings.HasPrefix(line, "- ") {
			val := strings.TrimSpace(line[2:])
			val = strings.Trim(val, `"'`)
			if val != "" {
				out = append(out, val)
			}
			consumed++
			continue
		}
		// 遇到非列表项，停止
		break
	}
	return out, consumed
}
