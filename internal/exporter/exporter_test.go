package exporter

import (
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		in   string
		want Format
	}{
		{"md", FormatMD},
		{"MD", FormatMD},
		{"txt", FormatTXT},
		{"TXT", FormatTXT},
		{"epub", FormatEPUB},
		{"pdf", FormatPDF},
		{"unknown", FormatMD}, // 默认 md
		{"", FormatMD},
	}
	for _, tc := range tests {
		if got := ParseFormat(tc.in); got != tc.want {
			t.Errorf("ParseFormat(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSupportedFormats(t *testing.T) {
	formats := SupportedFormats()
	if len(formats) != 4 {
		t.Errorf("expected 4 formats, got %d", len(formats))
	}
}

func TestExport_MD(t *testing.T) {
	r, err := Export("Title 1", "Body content", 1, FormatMD)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if r.Format != FormatMD {
		t.Errorf("expected format md, got %s", r.Format)
	}
	if r.MimeType != "text/markdown; charset=utf-8" {
		t.Errorf("wrong mimetype: %s", r.MimeType)
	}
	if !strings.Contains(string(r.Body), "Title 1") {
		t.Error("body should contain title")
	}
	if !strings.Contains(string(r.Body), "Body content") {
		t.Error("body should contain content")
	}
	if !strings.Contains(string(r.Body), "chapter: 1") {
		t.Error("body should contain frontmatter chapter")
	}
}

func TestExport_TXT(t *testing.T) {
	r, err := Export("Title", "**Bold** and *italic* and `code`", 2, FormatTXT)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	body := string(r.Body)
	if strings.Contains(body, "**") {
		t.Error("txt should strip ** bold markers")
	}
	if strings.Contains(body, "*italic*") {
		t.Error("txt should strip * italic markers")
	}
	if !strings.Contains(body, "Bold") {
		t.Error("body should contain plain text 'Bold'")
	}
}

func TestExport_EPUB(t *testing.T) {
	r, err := Export("Test Book", "Chapter content", 3, FormatEPUB)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	body := r.Body
	// EPUB 是 zip 格式
	if len(body) < 100 {
		t.Error("epub body too small")
	}
	// 检查 zip magic (PK\x03\x04)
	if body[0] != 'P' || body[1] != 'K' {
		t.Errorf("not a valid zip: starts with %x %x", body[0], body[1])
	}
	if r.MimeType != "application/epub+zip" {
		t.Errorf("wrong mimetype: %s", r.MimeType)
	}
}

func TestExport_PDF(t *testing.T) {
	r, err := Export("Test", "Hello world", 1, FormatPDF)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	body := r.Body
	// PDF magic
	if !strings.HasPrefix(string(body), "%PDF-1.4") {
		t.Errorf("not a valid PDF: starts with %q", body[:10])
	}
	if !strings.Contains(string(body), "%%EOF") {
		t.Errorf("PDF should end with %%EOF, got: %q", body[len(body)-20:])
	}
	if r.MimeType != "application/pdf" {
		t.Errorf("wrong mimetype: %s", r.MimeType)
	}
}

func TestExport_UnsupportedFormat(t *testing.T) {
	_, err := Export("t", "c", 1, Format("unknown"))
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestExport_ResultSize(t *testing.T) {
	r, err := Export("T", "content", 1, FormatMD)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if r.Size != len(r.Body) {
		t.Errorf("Size=%d != len(Body)=%d", r.Size, len(r.Body))
	}
}

func TestXMLEscape(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"<a>", "&lt;a&gt;"},
		{`"quote"`, "&quot;quote&quot;"},
		{"a&b", "a&amp;b"},
		{"it's", "it&apos;s"},
	}
	for _, tc := range tests {
		if got := xmlEscape(tc.in); got != tc.want {
			t.Errorf("xmlEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPDFEscape(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"(paren)", "\\(paren\\)"},
		{`back\slash`, `back\\slash`},
	}
	for _, tc := range tests {
		if got := pdfEscape(tc.in); got != tc.want {
			t.Errorf("pdfEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExportBookEPUB_Success(t *testing.T) {
	metadata := DefaultBookMetadata("测试书", "作者")
	chapters := []*Chapter{
		{ChapterNum: 1, Title: "第一章", Content: "第一章内容"},
		{ChapterNum: 2, Title: "第二章", Content: "第二章内容"},
	}
	result, err := ExportBookEPUB(metadata, chapters)
	if err != nil {
		t.Fatalf("ExportBookEPUB: %v", err)
	}
	if result.Size <= 0 {
		t.Error("Size should be > 0")
	}
	if !strings.Contains(result.Filename, "测试书") {
		t.Errorf("filename should contain title, got %s", result.Filename)
	}
	if !strings.Contains(string(result.Body), "mimetype") {
		t.Error("body should contain mimetype entry")
	}
	if !strings.Contains(string(result.Body), "chapter_001.xhtml") {
		t.Error("body should contain chapter 1 XHTML")
	}
	if !strings.Contains(string(result.Body), "chapter_002.xhtml") {
		t.Error("body should contain chapter 2 XHTML")
	}
	if !strings.Contains(string(result.Body), "nav.xhtml") {
		t.Error("body should contain nav.xhtml (EPUB 3 navigation)")
	}
}

func TestExportBookEPUB_ChapterTooLarge(t *testing.T) {
	metadata := DefaultBookMetadata("test", "author")
	// 创建 >5MB 的章节
	bigContent := strings.Repeat("x", MaxChapterBytes+1)
	chapters := []*Chapter{
		{ChapterNum: 1, Title: "ch1", Content: bigContent},
	}
	_, err := ExportBookEPUB(metadata, chapters)
	if _, ok := err.(*ChapterTooLargeError); !ok {
		t.Errorf("expected ChapterTooLargeError, got %T %v", err, err)
	}
}

func TestExportBookEPUB_BookTooLarge(t *testing.T) {
	metadata := DefaultBookMetadata("test", "author")
	// 30 章 × ~4MB = ~120MB > MaxBookBytes 100MB
	big := strings.Repeat("x", 4*1024*1024)
	chapters := make([]*Chapter, 30)
	for i := range chapters {
		chapters[i] = &Chapter{ChapterNum: i + 1, Title: "ch", Content: big}
	}
	_, err := ExportBookEPUB(metadata, chapters)
	if _, ok := err.(*BookTooLargeError); !ok {
		t.Errorf("expected BookTooLargeError, got %T %v", err, err)
	}
}

func TestExportBookEPUB_EmptyChapters(t *testing.T) {
	metadata := DefaultBookMetadata("test", "author")
	result, err := ExportBookEPUB(metadata, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Size <= 0 {
		t.Error("empty book should still produce EPUB with structure")
	}
	if metadata.TotalChapters != 0 {
		t.Errorf("TotalChapters = %d, want 0", metadata.TotalChapters)
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"正常名":  "正常名",
		"a/b":  "a_b",
		"a\\b": "a_b",
		"a:b":  "a_b",
		"a*b":  "a_b",
		"a?b":  "a_b",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestXMLEscape_All(t *testing.T) {
	// 简化测试: 只验证 xmlEscape 不 panic 且返回非空
	// (详细 entity 转义在 TestXMLEscape 中覆盖)
	in := "plain text with & and <>"
	got := xmlEscape(in)
	if len(got) == 0 {
		t.Error("xmlEscape returned empty string")
	}
	// 纯文本 (无特殊字符) 应该原样返回
	if got := xmlEscape("hello world"); got != "hello world" {
		t.Errorf("plain text changed: %q", got)
	}
	// < 应该被转义 (长度 > 1)
	if got := xmlEscape("<"); len(got) <= 1 {
		t.Errorf("< not escaped, got %q", got)
	}
}

func TestExportEPUB_TooLarge(t *testing.T) {
	// 单章 exportEPUB 路径: 触发 ChapterTooLargeError
	bigContent := strings.Repeat("x", MaxChapterBytes+1)
	_, err := exportEPUB("title", bigContent, 1)
	if _, ok := err.(*ChapterTooLargeError); !ok {
		t.Errorf("expected ChapterTooLargeError, got %T %v", err, err)
	}
}
