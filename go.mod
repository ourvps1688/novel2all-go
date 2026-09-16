module github.com/ourvps1688/novel2all-go

go 1.26.0

// P0 阶段无第三方依赖
// P1+ 阶段会引入：
//   github.com/gin-gonic/gin      (HTTP router)
//   github.com/mattn/go-sqlite3   (SQLite)
//   go.etcd.io/bbolt              (BoltDB)
//   github.com/philpgille/chromem-go (向量检索，替代 chromadb)
//   github.com/go-shogo/go-epub   (EPUB 导出)
//   github.com/jung-kurt/gofpdf   (PDF 导出)
//   github.com/anthropics/anthropic-sdk-go (LLM SDK)
//   github.com/sashabaranov/go-openai       (OpenAI 兼容)
//   github.com/instructor-go/instructor-go  (结构化输出)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.59.0 // indirect
)
