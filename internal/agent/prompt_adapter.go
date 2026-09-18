package agent

import (
	"regexp"
	"strings"
)

// AdaptPrompt 简化 vendor role .md body 适配中文 LLM (Sprint A1.9)
//
// vendor oh-story-dsh-0.1.9 的 role prompt 是为 Claude（Anthropic 官方）优化的：
//   - 大量 XML 标签（<thinking>、<output> 等）
//   - Claude 特定术语引用（"opus"、"sonnet"、"haiku"）
//   - 冗长的英语示例
//   - 复杂的 XML schema 定义
//
// 中文 LLM（MiniMax-M3 / DeepSeek V4 / 千问）偏好：
//   - 短 + 直接 + 中文优先
//   - 示例驱动（few-shot）优于规则说明
//   - 少用 XML 嵌套结构
//   - 不识别 "opus" 等 vendor 档位名（要替换成实际 model 名）
//
// AdaptPrompt 做的事：
//   1. 移除对 vendor 档位的硬编码引用（"opus" → 改写为说明性描述）
//   2. 简化常见 XML 标签为 markdown 或纯文本
//   3. 压缩连续空行
//   4. 把英文 "you are" 开头改写为中文 "你是"
//   5. 保留关键内容（不删创作指导）
//
// 注意：这是 heuristic，不是完美转换。中文 LLM 对 Claude prompt 的真实兼容度
// 还需要 A6 真实 LLM E2E 验证（A6.9 / A6.10 / A6.11）。
func AdaptPrompt(vendorPrompt string) string {
	p := vendorPrompt

	// 1. 替换 vendor 档位引用（opus/sonnet/haiku → 说明性描述）
	p = replaceVendorModelRefs(p)

	// 2. 简化 XML 标签（<thinking>...</thinking> → 思考：...）
	p = simplifyXMLTags(p)

	// 3. "You are" / "You must" / "You should" 开头 → 中文
	p = rewriteEnglishDirectives(p)

	// 4. 压缩连续空行（3+ → 2）
	p = collapseBlankLines(p)

	// 5. 修剪首尾空白
	return strings.TrimSpace(p)
}

// replaceVendorModelRefs 替换 vendor 档位名为说明性描述
//
// 例：
//   - "as opus, ..."     → "作为高质量写作模型, ..."
//   - "use sonnet for"   → "使用中档推理能力处理"
//   - "haiku role"       → "轻量级任务"
func replaceVendorModelRefs(p string) string {
	replacements := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		// opus → 高质量写作模型
		{regexp.MustCompile(`\bas\s+opus\b`), "作为高质量写作模型"},
		{regexp.MustCompile(`\bopus\s+model\b`), "高质量写作模型"},
		{regexp.MustCompile(`\bopus\b`), "高质量写作模型"},
		// sonnet → 中档推理模型
		{regexp.MustCompile(`\bsonnet\b`), "中档推理模型"},
		// haiku → 轻量级模型
		{regexp.MustCompile(`\bhaiku\b`), "轻量级模型"},
	}
	for _, r := range replacements {
		p = r.pattern.ReplaceAllString(p, r.replacement)
	}
	return p
}

// simplifyXMLTags 简化常见 XML 标签
//
// 支持的标签：
//   - <thinking>...</thinking> → 思考：...
//   - <output>...</output>     → 输出：...
//   - <example>...</example>   → 示例：...
//   - <warning>...</warning>   → 注意：...
//   - <important>...</important> → 重要：...
//   - 单标签 <tag/> → 移除
func simplifyXMLTags(p string) string {
	type tagRule struct {
		open  string
		close string
		prefx string
	}
	rules := []tagRule{
		{"<thinking>", "</thinking>", "思考："},
		{"<output>", "</output>", "输出："},
		{"<example>", "</example>", "示例："},
		{"<warning>", "</warning>", "注意："},
		{"<important>", "</important>", "重要："},
	}

	for _, r := range rules {
		// 简化：保留标签内文字，前面加 prefix
		openIdx := 0
		for {
			idx := strings.Index(p[openIdx:], r.open)
			if idx == -1 {
				break
			}
			idx += openIdx
			closeIdx := strings.Index(p[idx:], r.close)
			if closeIdx == -1 {
				break
			}
			closeIdx += idx
			// 替换 open + content + close 为 prefix + content
			content := p[idx+len(r.open) : closeIdx]
			p = p[:idx] + r.prefx + content + p[closeIdx+len(r.close):]
			openIdx = idx + len(r.prefx) + len(content)
		}
	}

	// 移除单标签 <tag/>
	re := regexp.MustCompile(`<[a-zA-Z][a-zA-Z0-9_-]*\s*/>`)
	p = re.ReplaceAllString(p, "")

	return p
}

// rewriteEnglishDirectives 把英文指令开头改成中文
//
// 只改首句的开头（避免误伤中间引用）。
func rewriteEnglishDirectives(p string) string {
	directives := []struct {
		pattern     string
		replacement string
	}{
		{"You are ", "你是"},
		{"You must ", "你必须"},
		{"You should ", "你应该"},
		{"You can ", "你可以"},
		{"You may ", "你可以"},
		{"Always ", "始终"},
		{"Never ", "绝不"},
	}

	for _, d := range directives {
		// 只匹配句首（行首或段首）
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(d.pattern))
		p = re.ReplaceAllString(p, d.replacement)
		// 段首（前面是换行 + 可能是 markdown 标题 #）
		re2 := regexp.MustCompile(`(?m)(^|\n)(#+\s+)?` + regexp.QuoteMeta(d.pattern))
		p = re2.ReplaceAllString(p, "${1}${2}"+d.replacement)
	}
	return p
}

// collapseBlankLines 把 3+ 连续空行压缩为 2 空行
func collapseBlankLines(p string) string {
	re := regexp.MustCompile(`\n{3,}`)
	return re.ReplaceAllString(p, "\n\n")
}

// NeedsAdaptation 判断 prompt 是否需要适配（快速检查，避免无谓处理）
//
// 启发：如果 prompt 不含任何 vendor 引用或 XML 标签，可能已经适配过。
func NeedsAdaptation(p string) bool {
	if strings.Contains(p, "opus") || strings.Contains(p, "sonnet") || strings.Contains(p, "haiku") {
		return true
	}
	if strings.Contains(p, "<thinking>") || strings.Contains(p, "<output>") {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(p), "You are ") {
		return true
	}
	return false
}
