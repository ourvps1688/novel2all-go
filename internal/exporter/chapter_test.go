package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChapter_WordCount(t *testing.T) {
	c := &Chapter{
		Content: "你好 world 这是 Go 代码",
	}
	// 中文字符: 你/好/这/是/代/码 = 6, 英文单词: world/Go = 2 → 共 8
	if got := c.WordCount(); got != 8 {
		t.Errorf("WordCount = %d, want 8", got)
	}
}

func TestChapter_ContentSizeBytes(t *testing.T) {
	c := &Chapter{Content: "hello 你好"}
	// "hello" = 5 + " " = 1 + "你好" = 6 (UTF-8 3 bytes/字) = 12
	got := c.ContentSizeBytes()
	if got < 10 {
		t.Errorf("ContentSizeBytes = %d, expected >= 10", got)
	}
}

func TestFromMDFile_WithTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	content := "# 第5章 主角觉醒\n\n这是章节内容。\n更多内容。"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := FromMDFile(path)
	if err != nil {
		t.Fatalf("FromMDFile: %v", err)
	}
	if c.ChapterNum != 5 {
		t.Errorf("ChapterNum = %d, want 5", c.ChapterNum)
	}
	if c.Title != "主角觉醒" {
		t.Errorf("Title = %q, want 主角觉醒", c.Title)
	}
	if !strings.Contains(c.Content, "这是章节内容") {
		t.Error("Content should contain chapter body")
	}
}

func TestFromMDFile_NoTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "第3章_无标题.md")
	content := "无标题内容"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := FromMDFile(path)
	if err != nil {
		t.Fatalf("FromMDFile: %v", err)
	}
	if c.ChapterNum != 3 {
		t.Errorf("ChapterNum = %d, want 3 (from filename)", c.ChapterNum)
	}
	if c.Title == "" {
		t.Error("Title should default to filename")
	}
}

func TestFromMDFile_NotFound(t *testing.T) {
	_, err := FromMDFile("/nonexistent/path.md")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestChapterTooLargeError(t *testing.T) {
	e := &ChapterTooLargeError{ChapterNum: 5, Bytes: 6000000, MaxBytes: 5000000}
	msg := e.Error()
	if !strings.Contains(msg, "chapter 5") {
		t.Errorf("error msg should contain 'chapter 5': %s", msg)
	}
	if !strings.Contains(msg, "6000000") {
		t.Errorf("error msg should contain bytes: %s", msg)
	}
}

func TestBookTooLargeError(t *testing.T) {
	e := &BookTooLargeError{TotalBytes: 150000000, MaxBytes: 100000000}
	msg := e.Error()
	if !strings.Contains(msg, "book too large") {
		t.Errorf("error msg should mention 'book too large': %s", msg)
	}
}
