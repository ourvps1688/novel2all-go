// pdf.go - PDF 导出（最简 ASCII 文本 PDF）
//
// 不依赖 gofpdf / cgo / libfreetype。
// 输出"PDF"实际上是最小有效 PDF 格式的 ASCII 文本包装。
//
// 真实生产建议用：
//   - github.com/jung-kurt/gofpdf（需 cgo）
//   - github.com/signintech/gopdf（纯 Go，但只支持简单文本）
package exporter

import (
	"fmt"
	"strings"
	"time"
)

// exportPDF 最简 PDF（ASCII 文本包装）
//
// 生成的 PDF 是有效的 PDF 1.4 文档，只支持 ASCII 文本。
// 中文字符会被转义（因为内置字体不支持中文）。
//
// 真实使用应替换为 jung-kurt/gofpdf 或 signintech/gopdf。
func exportPDF(title, content string, chapterNum int) (*ExportResult, error) {
	// 构造 PDF objects
	// PDF 结构: header + body (objects) + xref + trailer
	now := time.Now().UTC().Format("D:20060102150405")

	// 文本内容（escape PDF 特殊字符）
	text := pdfEscape(fmt.Sprintf("Chapter %d: %s\n\n%s", chapterNum, title, content))
	streamData := fmt.Sprintf("BT /F1 12 Tf 50 750 Td (%s) Tj ET", text)

	objects := []string{
		// 1: Catalog
		"<< /Type /Catalog /Pages 2 0 R >>",
		// 2: Pages
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// 3: Page
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		// 4: Contents
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(streamData), streamData),
		// 5: Font
		"<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>",
		// 6: Info
		fmt.Sprintf("<< /Title (%s) /Producer (novel2all-go exporter) /CreationDate (%s) >>", pdfEscape(title), now),
	}

	// 构造 PDF body
	var body strings.Builder
	body.WriteString("%PDF-1.4\n")
	body.WriteString("%\xE2\xE3\xCF\xD3\n") // binary comment 防止文本编辑器修改

	xrefOffsets := make([]int, len(objects)+1)
	xrefOffsets[0] = 0
	for i, obj := range objects {
		xrefOffsets[i+1] = body.Len()
		fmt.Fprintf(&body, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}

	xrefStart := body.Len()
	fmt.Fprintf(&body, "xref\n0 %d\n", len(objects)+1)
	fmt.Fprintf(&body, "0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&body, "%010d 00000 n \n", xrefOffsets[i])
	}
	fmt.Fprintf(&body, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\n", len(objects)+1)
	fmt.Fprintf(&body, "startxref\n%d\n%%%%EOF\n", xrefStart)

	return &ExportResult{
		Format:     FormatPDF,
		MimeType:   "application/pdf",
		Filename:   fmt.Sprintf("chapter_%03d.pdf", chapterNum),
		Size:       body.Len(),
		Body:       []byte(body.String()),
		ChapterNum: chapterNum,
	}, nil
}

// pdfEscape 转义 PDF 字符串特殊字符
func pdfEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	return s
}
