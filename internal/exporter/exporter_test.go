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
