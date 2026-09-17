// Package chroma 提供内存向量存储（替代 Python chroma/chromadb）。
//
// 设计目标：
//   - 纯 Go 实现，无 cgo 依赖（避免 chromem-go 的 cgo 编译问题）
//   - thread-safe（sync.RWMutex）
//   - 简单 hash-based embedding（SHA256 token hash → 128 维向量）
//   - cosine similarity top-K 查询
//   - P1 阶段只内存；P2 阶段可加 BoltDB 持久化
//
// 与 chromem-go / chroma / chromadb 的关系：
//   - API 概念类似（Document / Embed / Query / Upsert / Delete）
//   - embedding 算法不同（hash trick vs sentence-transformers）
//   - 性能不如真 ML，但 P1 阶段够用
//
// 真实生产建议用 sentence-transformers + chromem-go（chromem-go 有 cgo 依赖，需 CGO_ENABLED=1）
package chroma

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
)

// 默认向量维度（避免 import cycle，不引用 config）
const defaultDim = 128

// Document 文档结构
type Document struct {
	// ID 文档唯一 ID（SHA256 截前 16 hex 或用户自定义）
	ID string `json:"id"`
	// Text 文档原文
	Text string `json:"text"`
	// Metadata 任意元数据（filter / display）
	Metadata map[string]string `json:"metadata,omitempty"`
	// Vector embedding 向量（Upsert 时自动计算，不序列化）
	Vector []float32 `json:"-"`
}

// Match 搜索匹配
type Match struct {
	ID       string            `json:"id"`
	Text     string            `json:"text"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Score    float64           `json:"score"` // cosine 相似度，[-1, 1]
}

// Stats 存储统计
type Stats struct {
	DocumentCount int `json:"document_count"`
	VectorDim     int `json:"vector_dim"`
}

// ErrNotFound 文档未找到
var ErrNotFound = errors.New("chroma: document not found")

// Client 线程安全的内存向量存储
type Client struct {
	mu   sync.RWMutex
	docs map[string]*Document // id -> doc
	dim  int
}

// NewClient 创建 chroma client
//
// dim 向量维度（默认 128）
func NewClient(dim int) *Client {
	if dim <= 0 {
		dim = defaultDim
	}
	return &Client{
		docs: make(map[string]*Document),
		dim:  dim,
	}
}

// Dim 返回向量维度
func (c *Client) Dim() int {
	return c.dim
}

// Embed 文本 → 向量（hash trick）
//
// 算法：
//  1. 分词（小写 + 非字母数字分隔）
//  2. 每个 token SHA256 → 前 4 bytes mod dim → 累加
//  3. L2 normalize
//
// 优点：纯函数、无依赖、可重复
// 缺点：不捕捉语义（同义词不同 hash）
func (c *Client) Embed(text string) []float32 {
	v := make([]float32, c.dim)
	for _, tok := range tokenize(text) {
		h := sha256.Sum256([]byte(tok))
		idx := binary.BigEndian.Uint32(h[:4]) % uint32(c.dim)
		v[idx] += 1.0
	}
	// L2 normalize
	var norm float32
	for _, x := range v {
		norm += x * x
	}
	if norm > 0 {
		norm = float32(math.Sqrt(float64(norm)))
		for i := range v {
			v[i] /= norm
		}
	}
	return v
}

// Upsert 插入/更新文档
//
// docs[i].ID 为空时自动生成（text sha256 截前 16 hex）
// 返回生成的 ID 列表
func (c *Client) Upsert(docs []Document) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, 0, len(docs))
	for i := range docs {
		d := docs[i] // 复制避免外部修改
		if d.ID == "" {
			d.ID = generateID(d.Text)
		}
		d.Vector = c.Embed(d.Text)
		c.docs[d.ID] = &d
		ids = append(ids, d.ID)
	}
	return ids
}

// Get 取单个文档
func (c *Client) Get(id string) (*Document, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.docs[id]
	if !ok {
		return nil, ErrNotFound
	}
	// 返回拷贝
	cp := *d
	return &cp, nil
}

// Query top-K 相似搜索
//
// 返回 score 倒序的前 k 个匹配
func (c *Client) Query(text string, k int) []Match {
	if k <= 0 {
		k = 5
	}
	q := c.Embed(text)
	c.mu.RLock()
	defer c.mu.RUnlock()

	type scored struct {
		doc   *Document
		score float64
	}
	all := make([]scored, 0, len(c.docs))
	for _, d := range c.docs {
		all = append(all, scored{d, cosine(q, d.Vector)})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})
	if k > len(all) {
		k = len(all)
	}
	matches := make([]Match, k)
	for i := 0; i < k; i++ {
		matches[i] = Match{
			ID:       all[i].doc.ID,
			Text:     all[i].doc.Text,
			Metadata: all[i].doc.Metadata,
			Score:    all[i].score,
		}
	}
	return matches
}

// Delete 删除文档
func (c *Client) Delete(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.docs[id]; !ok {
		return ErrNotFound
	}
	delete(c.docs, id)
	return nil
}

// Clear 清空所有文档
func (c *Client) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.docs = make(map[string]*Document)
}

// Stats 统计
func (c *Client) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Stats{
		DocumentCount: len(c.docs),
		VectorDim:     c.dim,
	}
}

// tokenize 简单分词（小写 + 非字母数字分隔）
func tokenize(text string) []string {
	text = strings.ToLower(text)
	return strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}

// generateID 从 text 生成稳定 ID（sha256 截前 16 hex）
func generateID(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:8])
}

// cosine 余弦相似度
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
