module github.com/ourvps1688/novel2all-go

go 1.22

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