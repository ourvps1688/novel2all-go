package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// WebSearchTool Web 搜索工具 (Sprint A5.10)
//
// 当前为 MOCK 实现（A5.10 阶段）：
//   - 接受 query + optional max_results
//   - 返回预置的 mock 搜索结果
//   - 不实际调外部 search API
//
// 真实实现路线：
//   - A6 接 DuckDuckGo HTML scrape（无需 API key）
//   - 或接 Bing/Google Custom Search API（需要 key）
type WebSearchTool struct{}

// NewWebSearchTool 构造
func NewWebSearchTool() *WebSearchTool { return &WebSearchTool{} }

// Name 工具名
func (t *WebSearchTool) Name() string { return "WebSearch" }

// Description 工具描述
func (t *WebSearchTool) Description() string {
	return "搜索互联网。用 query 返回 top N 结果（title/url/snippet）。当前 MOCK 实现。"
}

// InputSchema JSON Schema
func (t *WebSearchTool) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "搜索关键词"},
    "max_results": {"type": "integer", "description": "最大返回结果数（默认 5）"}
  },
  "required": ["query"]
}`)
}

// Execute mock 实现：返回预置结果
func (t *WebSearchTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("WebSearch: invalid input: %v", err)), nil
	}
	if strings.TrimSpace(in.Query) == "" {
		return ErrorResult("WebSearch: query is required"), nil
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 5
	}
	if in.MaxResults > 10 {
		in.MaxResults = 10
	}

	// Mock 结果（A6 接真实 API 后替换）
	mockResults := []map[string]string{
		{"title": fmt.Sprintf("Result 1 for '%s'", in.Query), "url": "https://example.com/1", "snippet": "This is a mock result. Sprint A6 will integrate real search API."},
		{"title": fmt.Sprintf("Result 2 for '%s'", in.Query), "url": "https://example.com/2", "snippet": "Mock data — replace with DuckDuckGo scrape or Bing API."},
		{"title": fmt.Sprintf("Result 3 for '%s'", in.Query), "url": "https://example.com/3", "snippet": "Currently returns placeholder. Plan A5.10 mock until A6 real impl."},
	}
	if in.MaxResults < len(mockResults) {
		mockResults = mockResults[:in.MaxResults]
	}

	content := fmt.Sprintf("Search results for '%s':\n\n", in.Query)
	for i, r := range mockResults {
		content += fmt.Sprintf("%d. %s\n   URL: %s\n   %s\n\n", i+1, r["title"], r["url"], r["snippet"])
	}
	content += "NOTE: This is MOCK data. Sprint A6 will integrate real search API."
	return SuccessResult(content), nil
}
