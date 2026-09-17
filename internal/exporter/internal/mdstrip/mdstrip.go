// Package mdstrip 剥离 Markdown 标记，返回纯文本。
//
// 用途：小说章节 .md → .txt 导出
// 不依赖外部库，纯 regex 处理。
package mdstrip

import "regexp"

var (
	// 代码围栏（多行）：```...```
	fencedCode = regexp.MustCompile("(?s)```[^`]*```")
	// 行内代码：`xx`
	inlineCode = regexp.MustCompile("`([^`]*)`")
	// 图片：![alt](url)
	image = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	// 链接：[text](url) → text
	link = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	// 加粗：**xx** 或 __xx__
	bold1 = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	bold2 = regexp.MustCompile(`__([^_]+)__`)
	// 斜体：*xx* 或 _xx_（注意避免误匹配列表项）
	italic1 = regexp.MustCompile(`\*([^*]+)\*`)
	italic2 = regexp.MustCompile(`(?m)(^|\s)_([^_\n]+)_`)
	// 删除线：~~xx~~
	strike = regexp.MustCompile(`~~([^~]+)~~`)
	// 标题行：# / ## / ### 等
	heading = regexp.MustCompile(`(?m)^\s*#{1,6}\s+`)
	// 引用：> xx
	quote = regexp.MustCompile(`(?m)^\s*>\s?`)
	// 列表项：- xx 或 * xx 或 + xx
	listItem = regexp.MustCompile(`(?m)^\s*[-*+]\s+`)
	// 数字列表：1. xx
	orderedItem = regexp.MustCompile(`(?m)^\s*\d+\.\s+`)
	// 水平线：--- 或 *** 或 ___
	hr = regexp.MustCompile(`(?m)^[\s]*(---|\*\*\*|___)[\s]*$`)
	// 多个连续空行合并
	multiNewline = regexp.MustCompile(`\n{3,}`)
)

// Strip 移除 markdown 标记，返回纯文本
func Strip(s string) string {
	s = fencedCode.ReplaceAllString(s, "")
	s = inlineCode.ReplaceAllString(s, "$1")
	s = image.ReplaceAllString(s, "$1")
	s = link.ReplaceAllString(s, "$1")
	s = bold1.ReplaceAllString(s, "$1")
	s = bold2.ReplaceAllString(s, "$1")
	s = italic1.ReplaceAllString(s, "$1")
	s = italic2.ReplaceAllString(s, "$1 $2")
	s = strike.ReplaceAllString(s, "$1")
	s = heading.ReplaceAllString(s, "")
	s = quote.ReplaceAllString(s, "")
	s = listItem.ReplaceAllString(s, "")
	s = orderedItem.ReplaceAllString(s, "")
	s = hr.ReplaceAllString(s, "")
	s = multiNewline.ReplaceAllString(s, "\n\n")
	return s
}
