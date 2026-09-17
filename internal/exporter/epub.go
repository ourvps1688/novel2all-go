// epub.go - EPUB 导出 (Sprint 31: 单章 + 整书 + 完整 13 methods).
//
// EPUB 本质是 zip 包（含 mimetype / META-INF / OEBPS）。
// 这里用 archive/zip 手工打包（避免 go-epub 依赖）。
//
// EPUB 3.0 最小结构:
//   - mimetype             (uncompressed, "application/epub+zip")
//   - META-INF/container.xml
//   - OEBPS/content.opf
//   - OEBPS/toc.ncx
//   - OEBPS/nav.xhtml       (EPUB 3 navigation)
//   - OEBPS/chapter_NNN.xhtml
//
// V1.0.2 B5 OOM 防护 (对齐 Python):
//   - 单章 > 5 MB 拒绝 (返回 ChapterTooLargeError)
//   - 整书 > 100 MB 拒绝 (返回 BookTooLargeError)
//
// 不支持的特性:
//   - 嵌入字体（纯文本 + CSS）
//   - 图片（不嵌 base64）
//   - 复杂 nav.xhtml（用 toc.ncx 简化）
//
// 真实生产建议用 github.com/go-shogo/go-epub.
package exporter

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"
)

// MaxChapterBytes 单章上限 (V1.0.2 B5 OOM 防护, 5 MB).
const MaxChapterBytes = 5 * 1024 * 1024

// MaxBookBytes 整书上限 (V1.0.2 B5 OOM 防护, 100 MB).
const MaxBookBytes = 100 * 1024 * 1024

// exportEPUB 单章 EPUB 导出 (Sprint 24 V0 简化版, 保留兼容).
func exportEPUB(title, content string, chapterNum int) (*ExportResult, error) {
	// V0.27.3: 单章大小预检
	if len(content) > MaxChapterBytes {
		return nil, &ChapterTooLargeError{
			ChapterNum: chapterNum,
			Bytes:      len(content),
			MaxBytes:   MaxChapterBytes,
		}
	}

	chapters := []*Chapter{{ChapterNum: chapterNum, Title: title, Content: content}}
	book, err := ExportBookEPUB(DefaultBookMetadata(title, "novel2all"), chapters)
	if err != nil {
		return nil, err
	}
	return &ExportResult{
		Format:     FormatEPUB,
		MimeType:   "application/epub+zip",
		Filename:   fmt.Sprintf("chapter_%03d.epub", chapterNum),
		Size:       book.Size,
		Body:       book.Body,
		ChapterNum: chapterNum,
	}, nil
}

// ExportBookEPUB 整书 EPUB 导出 (Sprint 31 新功能, 对齐 Python export_book).
//
// metadata: 书元数据.
// chapters: 所有章节 (按 ChapterNum ASC).
//
// 流程:
//  1. 预检所有章节大小 (ChapterTooLargeError)
//  2. 预检总大小 (BookTooLargeError)
//  3. Build EPUB ZIP (container.xml + content.opf + toc.ncx + nav.xhtml + chapters)
func ExportBookEPUB(metadata *BookMetadata, chapters []*Chapter) (*BookResult, error) {
	// 1. 预检单章
	for _, c := range chapters {
		if c.ContentSizeBytes() > MaxChapterBytes {
			return nil, &ChapterTooLargeError{
				ChapterNum: c.ChapterNum,
				Bytes:      c.ContentSizeBytes(),
				MaxBytes:   MaxChapterBytes,
			}
		}
	}
	// 2. 预检总大小
	totalBytes := TotalBytes(chapters)
	if totalBytes > MaxBookBytes {
		return nil, &BookTooLargeError{TotalBytes: totalBytes, MaxBytes: MaxBookBytes}
	}
	// 3. 自动算 TotalChapters + TotalWords
	metadata.ComputeTotals(chapters)

	// 4. Build ZIP
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	if err := writeMimetype(zw); err != nil {
		return nil, fmt.Errorf("write mimetype: %w", err)
	}
	if err := writeContainerXML(zw); err != nil {
		return nil, fmt.Errorf("write container.xml: %w", err)
	}
	if err := writeContentOPF(zw, metadata, chapters); err != nil {
		return nil, fmt.Errorf("write content.opf: %w", err)
	}
	if err := writeTocNCX(zw, metadata, chapters); err != nil {
		return nil, fmt.Errorf("write toc.ncx: %w", err)
	}
	if err := writeNavXHTML(zw, metadata, chapters); err != nil {
		return nil, fmt.Errorf("write nav.xhtml: %w", err)
	}
	for i, c := range chapters {
		if err := writeChapterXHTML(zw, c, i+1); err != nil {
			return nil, fmt.Errorf("write chapter %d: %w", c.ChapterNum, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}

	return &BookResult{
		Filename: fmt.Sprintf("%s.epub", sanitizeFilename(metadata.Title)),
		Size:     buf.Len(),
		Body:     buf.Bytes(),
	}, nil
}

// BookResult 整书导出结果.
type BookResult struct {
	Filename string
	Size     int
	Body     []byte
}

// Sprint 31 helper methods (13 methods 对齐 Python EPUBExporter):

// writeMimetype 写 mimetype (单章 + 整书都用).
func writeMimetype(zw *zip.Writer) error {
	w, err := zw.Create("mimetype")
	if err != nil {
		return err
	}
	_, err = w.Write([]byte("application/epub+zip"))
	return err
}

// _buildContainerXML 构造 container.xml.
func _buildContainerXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`
}

// writeContainerXML 写 META-INF/container.xml.
func writeContainerXML(zw *zip.Writer) error {
	w, err := zw.Create("META-INF/container.xml")
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(_buildContainerXML()))
	return err
}

// _buildOPF 构造 content.opf (manifest + spine).
func _buildOPF(metadata *BookMetadata, chapters []*Chapter) string {
	identifier := metadata.Identifier
	if identifier == "" {
		identifier = fmt.Sprintf("novel2all-%d", time.Now().UnixNano())
	}
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid" xml:lang="`)
	sb.WriteString(metadata.LanguageCode)
	sb.WriteString(`">
  <metadata>
    <dc:identifier id="bookid">urn:uuid:`)
	sb.WriteString(xmlEscape(identifier))
	sb.WriteString(`</dc:identifier>
    <dc:title>`)
	sb.WriteString(xmlEscape(metadata.Title))
	sb.WriteString(`</dc:title>
    <dc:language>`)
	sb.WriteString(metadata.LanguageCode)
	sb.WriteString(`</dc:language>
    <dc:creator>`)
	sb.WriteString(xmlEscape(metadata.Author))
	sb.WriteString(`</dc:creator>
    <dc:description>`)
	sb.WriteString(xmlEscape(metadata.Description))
	sb.WriteString(`</dc:description>
    <dc:publisher>`)
	sb.WriteString(xmlEscape(metadata.Publisher))
	sb.WriteString(`</dc:publisher>
    <dc:date>`)
	sb.WriteString(metadata.CreatedAt.Format("2006-01-02"))
	sb.WriteString(`</dc:date>
    <meta property="dcterms:modified">`)
	sb.WriteString(metadata.CreatedAt.Format(time.RFC3339))
	sb.WriteString(`</meta>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
`)
	for i := range chapters {
		fmt.Fprintf(&sb, `    <item id="chapter%d" href="chapter_%03d.xhtml" media-type="application/xhtml+xml"/>
`, i+1, i+1)
	}
	sb.WriteString(`  </manifest>
  <spine toc="ncx">
`)
	for i := range chapters {
		fmt.Fprintf(&sb, `    <itemref idref="chapter%d"/>
`, i+1)
	}
	sb.WriteString(`  </spine>
</package>`)
	return sb.String()
}

// writeContentOPF 写 OEBPS/content.opf.
func writeContentOPF(zw *zip.Writer, metadata *BookMetadata, chapters []*Chapter) error {
	w, err := zw.Create("OEBPS/content.opf")
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(_buildOPF(metadata, chapters)))
	return err
}

// _buildNCX 构造 toc.ncx (legacy navigation).
func _buildNCX(metadata *BookMetadata, chapters []*Chapter) string {
	identifier := metadata.Identifier
	if identifier == "" {
		identifier = fmt.Sprintf("novel2all-%d", time.Now().UnixNano())
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head>
    <meta name="dtb:uid" content="urn:uuid:%s"/>
    <meta name="dtb:depth" content="1"/>
    <meta name="dtb:totalPageCount" content="%d"/>
  </head>
  <docTitle>
    <text>%s</text>
  </docTitle>
  <navMap>
`, identifier, len(chapters), xmlEscape(metadata.Title))
	for i, c := range chapters {
		fmt.Fprintf(&sb, `    <navPoint id="ch%d" playOrder="%d">
      <navLabel>
        <text>%s</text>
      </navLabel>
      <content src="chapter_%03d.xhtml"/>
    </navPoint>
`, i+1, i+1, xmlEscape(c.Title), i+1)
	}
	sb.WriteString(`  </navMap>
</ncx>`)
	return sb.String()
}

// writeTocNCX 写 OEBPS/toc.ncx.
func writeTocNCX(zw *zip.Writer, metadata *BookMetadata, chapters []*Chapter) error {
	w, err := zw.Create("OEBPS/toc.ncx")
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(_buildNCX(metadata, chapters)))
	return err
}

// _buildNav 构造 nav.xhtml (EPUB 3 navigation).
func _buildNav(metadata *BookMetadata, chapters []*Chapter) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head>
  <title>%s</title>
</head>
<body>
  <nav epub:type="toc">
    <h1>目录</h1>
    <ol>
`, xmlEscape(metadata.Title))
	for i, c := range chapters {
		fmt.Fprintf(&sb, `      <li><a href="chapter_%03d.xhtml">%s</a></li>
`, i+1, xmlEscape(c.Title))
	}
	sb.WriteString(`    </ol>
  </nav>
</body>
</html>`)
	return sb.String()
}

// writeNavXHTML 写 OEBPS/nav.xhtml.
func writeNavXHTML(zw *zip.Writer, metadata *BookMetadata, chapters []*Chapter) error {
	w, err := zw.Create("OEBPS/nav.xhtml")
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(_buildNav(metadata, chapters)))
	return err
}

// _buildChapterXHTML 构造单章 XHTML.
func _buildChapterXHTML(c *Chapter, index int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <title>%s</title>
  <link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
  <h1>%s</h1>
  <pre>%s</pre>
</body>
</html>`, xmlEscape(c.Title), xmlEscape(c.Title), xmlEscape(c.Content))
}

// writeChapterXHTML 写 OEBPS/chapter_NNN.xhtml.
func writeChapterXHTML(zw *zip.Writer, c *Chapter, index int) error {
	w, err := zw.Create(fmt.Sprintf("OEBPS/chapter_%03d.xhtml", index))
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(_buildChapterXHTML(c, index)))
	return err
}

// _checkChapterSize 检查单章大小.
func _checkChapterSize(c *Chapter) error {
	if c.ContentSizeBytes() > MaxChapterBytes {
		return &ChapterTooLargeError{
			ChapterNum: c.ChapterNum,
			Bytes:      c.ContentSizeBytes(),
			MaxBytes:   MaxChapterBytes,
		}
	}
	return nil
}

// _checkBookSize 检查整书大小.
func _checkBookSize(chapters []*Chapter) error {
	total := TotalBytes(chapters)
	if total > MaxBookBytes {
		return &BookTooLargeError{TotalBytes: total, MaxBytes: MaxBookBytes}
	}
	return nil
}

// _coverPage 构造封面页 XHTML (V0.27.3 Sprint 31 新增).
func _coverPage(metadata *BookMetadata) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <title>Cover</title>
</head>
<body>
  <h1 style="text-align:center;">%s</h1>
  <p style="text-align:center;font-size:large;">%s</p>
  <p style="text-align:center;">共 %d 章 / %d 字</p>
</body>
</html>`, xmlEscape(metadata.Title), xmlEscape(metadata.Author), metadata.TotalChapters, metadata.TotalWords)
}

// _zipFiles 批量加文件 (V0.27.3 Sprint 31 工具方法, 当前未使用).
//
// 保留供未来 batch 优化 (直接传入 map[string][]byte).
func _zipFiles(zw *zip.Writer, files map[string][]byte) error {
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// sanitizeFilename 文件名清洗 (替换非法字符).
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", `"`, "_", "<", "_", ">", "_", "|", "_",
	)
	return replacer.Replace(name)
}

// xmlEscape 转义 XML 特殊字符 (避免 raw string 引号冲突).
func xmlEscape(s string) string {
	out := s
	out = replaceAllByte(out, 0x26, []byte{0x26, 0x61, 0x6D, 0x70, 0x3B})       // & -> amp
	out = replaceAllByte(out, 0x3C, []byte{0x26, 0x6C, 0x74, 0x3B})             // < -> lt
	out = replaceAllByte(out, 0x3E, []byte{0x26, 0x67, 0x74, 0x3B})             // > -> gt
	out = replaceAllByte(out, 0x22, []byte{0x26, 0x71, 0x75, 0x6F, 0x74, 0x3B}) // " -> quot
	out = replaceAllByte(out, 0x27, []byte{0x26, 0x61, 0x70, 0x6F, 0x73, 0x3B}) // ' -> apos
	return out
}

// replaceAllByte 单字符替换 (避免 strings.ReplaceAll 的转义问题).
func replaceAllByte(s string, old byte, newStr []byte) string {
	if old == 0 {
		return s
	}
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == old {
			out = append(out, newStr...)
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}
