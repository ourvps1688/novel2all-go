// Package exporter: metadata.go — Sprint 31 BookMetadata.
//
// 对齐 Python V1.0.2 B5 core/exporter.py BookMetadata.
package exporter

import "time"

// BookMetadata 整书元数据 (EPUB 封面 + OPF manifest 用).
type BookMetadata struct {
	Title         string
	Author        string
	Language      string    // BCP-47, e.g. "zh-CN" / "en-US"
	Genre         string    // 玄幻 / 都市 / 言情 / etc.
	Description   string    // 简介
	Publisher     string    // 出版社 (默认 "novel2all")
	TotalChapters int       // 总章节数 (留 -1 = 自动从 chapters 算)
	TotalWords    int       // 总字数 (留 -1 = 自动算)
	CreatedAt     time.Time // 创建时间
	Identifier    string    // UUID (留空自动生成)
	LanguageCode  string    // ISO 639-1, e.g. "zh" / "en" (默认从 Language 推导)
}

// DefaultBookMetadata 返回默认元数据 (中文 + 自定义 Title/Author).
func DefaultBookMetadata(title, author string) *BookMetadata {
	return &BookMetadata{
		Title:         title,
		Author:        author,
		Language:      "zh-CN",
		LanguageCode:  "zh",
		Genre:         "小说",
		Description:   "",
		Publisher:     "novel2all",
		TotalChapters: -1,
		TotalWords:    -1,
		CreatedAt:     time.Now().UTC(),
		Identifier:    "",
	}
}

// ComputeTotals 从 chapters 自动算 TotalChapters + TotalWords (如未设置).
func (m *BookMetadata) ComputeTotals(chapters []*Chapter) {
	if m.TotalChapters < 0 {
		m.TotalChapters = len(chapters)
	}
	if m.TotalWords < 0 {
		total := 0
		for _, c := range chapters {
			total += c.WordCount()
		}
		m.TotalWords = total
	}
}

// TotalBytes 所有 chapter 内容总字节数 (用于 BookTooLargeError 预检).
func TotalBytes(chapters []*Chapter) int {
	total := 0
	for _, c := range chapters {
		total += c.ContentSizeBytes()
	}
	return total
}
