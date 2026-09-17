// Package exporter 提供小说内容导出功能（md/txt/epub/pdf）。
//
// 迁移自 Python core/exporter.py（540 行）。
//
// 设计目标：
//   - 简单、无外部 ML 依赖
//   - 4 种格式：Markdown（原始）、纯文本（去 markdown 标记）、EPUB、PDF
//   - EPUB 用 zip + XHTML 手工打包（避免 go-epub cgo 依赖）
//   - PDF 用纯文本表格格式（避免 gofpdf cgo 依赖）
//
// 真实生产建议：
//   - PDF 用 jung-kurt/gofpdf（需 CGO_ENABLED=1）
//   - EPUB 用 go-shogo/go-epub
package exporter

import (
	"fmt"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/exporter/internal/mdstrip"
)

// Format 导出格式枚举
type Format string

const (
	FormatMD   Format = "md"
	FormatTXT  Format = "txt"
	FormatEPUB Format = "epub"
	FormatPDF  Format = "pdf"
)

// ParseFormat 解析 format 字符串（默认 md）
func ParseFormat(s string) Format {
	switch strings.ToLower(s) {
	case "txt":
		return FormatTXT
	case "epub":
		return FormatEPUB
	case "pdf":
		return FormatPDF
	default:
		return FormatMD
	}
}

// ExportResult 导出结果
type ExportResult struct {
	Format     Format `json:"format"`
	MimeType   string `json:"mime_type"`
	Filename   string `json:"filename"`
	Size       int    `json:"size"`
	Body       []byte `json:"-"` // 实际内容（不序列化）
	ChapterNum int    `json:"chapter_num,omitempty"`
}

// Export 单章节导出
func Export(title, content string, chapterNum int, format Format) (*ExportResult, error) {
	switch format {
	case FormatMD:
		return exportMD(title, content, chapterNum)
	case FormatTXT:
		return exportTXT(title, content, chapterNum)
	case FormatEPUB:
		return exportEPUB(title, content, chapterNum)
	case FormatPDF:
		return exportPDF(title, content, chapterNum)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// exportMD Markdown 导出（直接返回原文 + YAML frontmatter）
func exportMD(title, content string, chapterNum int) (*ExportResult, error) {
	body := []byte(fmt.Sprintf("---\ntitle: %s\nchapter: %d\n---\n\n%s", title, chapterNum, content))
	return &ExportResult{
		Format:     FormatMD,
		MimeType:   "text/markdown; charset=utf-8",
		Filename:   fmt.Sprintf("chapter_%03d.md", chapterNum),
		Size:       len(body),
		Body:       body,
		ChapterNum: chapterNum,
	}, nil
}

// exportTXT 纯文本导出（剥离 markdown 标记）
func exportTXT(title, content string, chapterNum int) (*ExportResult, error) {
	body := []byte(fmt.Sprintf("%s\n\n%s\n", title, mdstrip.Strip(content)))
	return &ExportResult{
		Format:     FormatTXT,
		MimeType:   "text/plain; charset=utf-8",
		Filename:   fmt.Sprintf("chapter_%03d.txt", chapterNum),
		Size:       len(body),
		Body:       body,
		ChapterNum: chapterNum,
	}, nil
}

// SupportedFormats 返回支持的格式列表
func SupportedFormats() []Format {
	return []Format{FormatMD, FormatTXT, FormatEPUB, FormatPDF}
}
