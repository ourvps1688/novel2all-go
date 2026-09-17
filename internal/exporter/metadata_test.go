package exporter

import (
	"strings"
	"testing"
)

func TestDefaultBookMetadata(t *testing.T) {
	m := DefaultBookMetadata("书名", "作者")
	if m.Title != "书名" {
		t.Errorf("Title = %q", m.Title)
	}
	if m.Author != "作者" {
		t.Errorf("Author = %q", m.Author)
	}
	if m.LanguageCode != "zh" {
		t.Errorf("LanguageCode = %q, want zh", m.LanguageCode)
	}
	if m.TotalChapters != -1 {
		t.Errorf("TotalChapters = %d, want -1", m.TotalChapters)
	}
	if m.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestBookMetadata_ComputeTotals(t *testing.T) {
	m := DefaultBookMetadata("test", "author")
	chapters := []*Chapter{
		{ChapterNum: 1, Title: "ch1", Content: "你好世界"},
		{ChapterNum: 2, Title: "ch2", Content: "hello world Go"},
	}
	m.ComputeTotals(chapters)
	if m.TotalChapters != 2 {
		t.Errorf("TotalChapters = %d, want 2", m.TotalChapters)
	}
	// "你好世界" = 4 中文字 + 0 英文 = 4
	// "hello world Go" = 0 中文 + 3 英文 = 3
	if m.TotalWords != 7 {
		t.Errorf("TotalWords = %d, want 7", m.TotalWords)
	}
}

func TestBookMetadata_ComputeTotals_PreserveExisting(t *testing.T) {
	m := DefaultBookMetadata("test", "author")
	m.TotalChapters = 100 // user override
	m.TotalWords = 99999
	m.ComputeTotals(nil)
	if m.TotalChapters != 100 {
		t.Errorf("existing TotalChapters should be preserved, got %d", m.TotalChapters)
	}
	if m.TotalWords != 99999 {
		t.Errorf("existing TotalWords should be preserved, got %d", m.TotalWords)
	}
}

func TestTotalBytes(t *testing.T) {
	chapters := []*Chapter{
		{Content: "abc"},
		{Content: "你好"}, // 6 bytes UTF-8 (3 bytes × 2 chars)
	}
	got := TotalBytes(chapters)
	if got != 9 {
		t.Errorf("TotalBytes = %d, want 9 (3 + 6)", got)
	}
}

func TestBookTooLargeError_String(t *testing.T) {
	e := &BookTooLargeError{TotalBytes: 150, MaxBytes: 100}
	msg := e.Error()
	if !strings.Contains(msg, "150") || !strings.Contains(msg, "100") {
		t.Errorf("error msg: %s", msg)
	}
}
