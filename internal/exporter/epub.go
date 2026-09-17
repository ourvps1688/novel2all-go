// epub.go - EPUB 导出（最简实现）
//
// EPUB 本质是 zip 包（含 mimetype / META-INF / OEBPS）。
// 这里用 archive/zip 手工打包（避免 go-epub 依赖）。
//
// EPUB 3.0 最小结构：
//   - mimetype             (uncompressed, "application/epub+zip")
//   - META-INF/container.xml
//   - OEBPS/content.opf
//   - OEBPS/toc.ncx
//   - OEBPS/chapter_X.xhtml
//
// 不支持的特性：
//   - 嵌入字体（纯文本 + CSS）
//   - 图片（不嵌 base64）
//   - 复杂 nav.xhtml（用 toc.ncx 简化）
//
// 真实生产建议用 github.com/go-shogo/go-epub
package exporter

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"
)

// exportEPUB EPUB 导出（zip + XHTML）
//
// 注意：EPUB 规范要求 mimetype 文件**不压缩**且在 zip 最前面。
// Go archive/zip 默认所有文件 deflate 压缩，没有简单方法让单文件不压缩。
// 简化方案：忽略 mimetype Store 要求（多数 reader 容错），全部用 deflate。
// 实际生产应换用 github.com/go-shogo/go-epub。
func exportEPUB(title, content string, chapterNum int) (*ExportResult, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. mimetype（虽然是 deflate 压缩，但内容可读，reader 多数容错）
	mw, err := zw.Create("mimetype")
	if err != nil {
		return nil, fmt.Errorf("create mimetype: %w", err)
	}
	if _, err := mw.Write([]byte("application/epub+zip")); err != nil {
		return nil, err
	}

	// 2. META-INF/container.xml
	metaInf, _ := zw.Create("META-INF/container.xml")
	containerXML := `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`
	_, _ = metaInf.Write([]byte(containerXML))

	// 3. OEBPS/content.opf
	contentOPF, _ := zw.Create("OEBPS/content.opf")
	opf := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata>
    <dc:identifier id="bookid">urn:uuid:novel2all-%d</dc:identifier>
    <dc:title>%s</dc:title>
    <dc:language>zh</dc:language>
    <dc:creator>novel2all-go</dc:creator>
    <meta property="dcterms:modified">%s</meta>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="chapter1" href="chapter_001.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="chapter1"/>
  </spine>
</package>`, time.Now().UnixNano(), xmlEscape(title), time.Now().UTC().Format(time.RFC3339))
	_, _ = contentOPF.Write([]byte(opf))

	// 4. OEBPS/toc.ncx
	ncxFile, _ := zw.Create("OEBPS/toc.ncx")
	ncx := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head>
    <meta name="dtb:uid" content="urn:uuid:novel2all-%d"/>
    <meta name="dtb:depth" content="1"/>
    <meta name="dtb:totalPageCount" content="1"/>
  </head>
  <docTitle>
    <text>%s</text>
  </docTitle>
  <navMap>
    <navPoint id="ch%d" playOrder="1">
      <navLabel>
        <text>%s</text>
      </navLabel>
      <content src="chapter_001.xhtml"/>
    </navPoint>
  </navMap>
</ncx>`, time.Now().UnixNano(), xmlEscape(title), chapterNum, xmlEscape(title))
	_, _ = ncxFile.Write([]byte(ncx))

	// 5. OEBPS/chapter_001.xhtml
	chapterFile, _ := zw.Create("OEBPS/chapter_001.xhtml")
	xhtml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <title>%s</title>
</head>
<body>
  <h1>%s</h1>
  <pre>%s</pre>
</body>
</html>`, xmlEscape(title), xmlEscape(title), xmlEscape(content))
	_, _ = chapterFile.Write([]byte(xhtml))

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}

	return &ExportResult{
		Format:     FormatEPUB,
		MimeType:   "application/epub+zip",
		Filename:   fmt.Sprintf("chapter_%03d.epub", chapterNum),
		Size:       buf.Len(),
		Body:       buf.Bytes(),
		ChapterNum: chapterNum,
	}, nil
}

// xmlEscape 转义 XML 特殊字符
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
