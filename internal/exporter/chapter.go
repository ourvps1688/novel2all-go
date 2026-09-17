// Package exporter: chapter.go — Sprint 31 Chapter 数据模型.
//
// 对齐 Python V1.0.2 B5 core/exporter.py Chapter class.
package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Chapter 单章数据模型.
type Chapter struct {
	ChapterNum int
	Title      string
	Content    string
	// SourcePath 源 markdown 文件路径 (可选, 调试用).
	SourcePath string
}

// WordCount 返回字数 (中文字符 + 英文单词).
func (c *Chapter) WordCount() int {
	zhCount := len(zhCharRegex.FindAllString(c.Content, -1))
	enCount := len(enWordRegex.FindAllString(c.Content, -1))
	return zhCount + enCount
}

// ContentSizeBytes 返回 UTF-8 编码字节数 (用于 EPUB 大小预检).
func (c *Chapter) ContentSizeBytes() int {
	return len(c.Content) // Go 默认 UTF-8, len(string) 返回字节数
}

var (
	zhCharRegex = regexp.MustCompile(`[\p{Han}]`)
	enWordRegex = regexp.MustCompile(`[a-zA-Z]+`)
)

// FromMDFile 从 markdown 文件加载章节.
//
// 解析规则:
//   - 首行 `# 第N章 标题` 提取 ChapterNum + Title
//   - 剩余行作为 Content (不含首行)
func FromMDFile(path string) (*Chapter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read chapter file %s: %w", path, err)
	}
	text := string(data)
	lines := strings.SplitN(text, "\n", 2)

	firstLine := strings.TrimSpace(lines[0])
	c := &Chapter{Content: text, SourcePath: path}

	// 匹配 `# 第N章 标题`
	if m := chapterTitleRegex.FindStringSubmatch(firstLine); len(m) == 3 {
		var num int
		_, _ = fmt.Sscanf(m[1], "%d", &num)
		c.ChapterNum = num
		c.Title = m[2]
		if len(lines) > 1 {
			c.Content = strings.TrimLeft(lines[1], "\n")
		}
	} else {
		// 无标题行: 从文件名推断章节号
		var num int
		basename := filepath.Base(path)
		if m := fileChapterRegex.FindStringSubmatch(basename); len(m) == 2 {
			_, _ = fmt.Sscanf(m[1], "%d", &num)
			c.ChapterNum = num
		}
		c.Title = strings.TrimSuffix(basename, filepath.Ext(basename))
	}

	return c, nil
}

var (
	chapterTitleRegex = regexp.MustCompile(`^#\s*第(\d+)章[：:\s]*(.*)$`)
	fileChapterRegex  = regexp.MustCompile(`第(\d+)章`)
)

// ChapterTooLargeError 单章过大错误 (V1.0.2 B5 EPUB OOM 防护).
type ChapterTooLargeError struct {
	ChapterNum int
	Bytes      int
	MaxBytes   int
}

func (e *ChapterTooLargeError) Error() string {
	return fmt.Sprintf("chapter %d too large: %d bytes > %d max", e.ChapterNum, e.Bytes, e.MaxBytes)
}

// BookTooLargeError 整书过大错误.
type BookTooLargeError struct {
	TotalBytes int
	MaxBytes   int
}

func (e *BookTooLargeError) Error() string {
	return fmt.Sprintf("book too large: %d bytes > %d max", e.TotalBytes, e.MaxBytes)
}
