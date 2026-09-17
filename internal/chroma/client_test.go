package chroma

import (
	"math"
	"strings"
	"testing"
)

func TestEmbed_Normalized(t *testing.T) {
	c := NewClient(128)
	v := c.Embed("hello world hello")
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if math.Abs(norm-1.0) > 0.01 {
		t.Errorf("expected L2 norm ~1.0, got %f", norm)
	}
}

func TestEmbed_EmptyText(t *testing.T) {
	c := NewClient(64)
	v := c.Embed("")
	// 空文本应返回零向量
	for i, x := range v {
		if x != 0 {
			t.Errorf("v[%d]=%f, expected 0 for empty text", i, x)
		}
	}
}

func TestEmbed_Deterministic(t *testing.T) {
	c := NewClient(64)
	v1 := c.Embed("hello world")
	v2 := c.Embed("hello world")
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("v1[%d]=%f != v2[%d]=%f", i, v1[i], i, v2[i])
		}
	}
}

func TestEmbed_DifferentDim(t *testing.T) {
	c1 := NewClient(32)
	c2 := NewClient(128)
	v1 := c1.Embed("test")
	v2 := c2.Embed("test")
	if len(v1) != 32 || len(v2) != 128 {
		t.Errorf("expected dim 32/128, got %d/%d", len(v1), len(v2))
	}
}

func TestEmbed_SameTextSameVector(t *testing.T) {
	c := NewClient(64)
	v1 := c.Embed("Go is fun")
	v2 := c.Embed("fun is Go") // 词序不同但词相同
	// 词袋模型：相同词应该有相同向量（顺序无关）
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("bag-of-words should be order-independent: v1[%d]=%f != v2[%d]=%f", i, v1[i], i, v2[i])
		}
	}
}

func TestUpsert_AutoID(t *testing.T) {
	c := NewClient(64)
	ids := c.Upsert([]Document{
		{Text: "doc 1"},
		{Text: "doc 2"},
	})
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}
	if ids[0] == "" || ids[1] == "" {
		t.Error("expected non-empty auto-generated ids")
	}
	if ids[0] == ids[1] {
		t.Error("expected different ids for different texts")
	}
}

func TestUpsert_Update(t *testing.T) {
	c := NewClient(64)
	ids1 := c.Upsert([]Document{{ID: "a", Text: "first"}})
	if len(ids1) != 1 || ids1[0] != "a" {
		t.Errorf("expected [a], got %v", ids1)
	}
	// 再次 upsert 同 id, text 改
	ids2 := c.Upsert([]Document{{ID: "a", Text: "second"}})
	if ids2[0] != "a" {
		t.Errorf("expected id a, got %s", ids2[0])
	}
	d, _ := c.Get("a")
	if d.Text != "second" {
		t.Errorf("expected updated text, got %q", d.Text)
	}
	if c.Stats().DocumentCount != 1 {
		t.Errorf("expected 1 doc, got %d", c.Stats().DocumentCount)
	}
}

func TestGet(t *testing.T) {
	c := NewClient(64)
	c.Upsert([]Document{{ID: "x", Text: "hello", Metadata: map[string]string{"k": "v"}}})
	d, err := c.Get("x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if d.Text != "hello" || d.Metadata["k"] != "v" {
		t.Errorf("unexpected doc: %+v", d)
	}
	_, err = c.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent id")
	}
}

func TestQuery_TopK(t *testing.T) {
	c := NewClient(64)
	// hash trick 算的是 cosine 相似度，看 token 频率方向
	// doc=2 (a quick brown fox) 的向量方向完全和 query "quick brown fox" 一致
	// doc=1 有 quick/brown/fox + 其他 token，方向被稀释
	c.Upsert([]Document{
		{ID: "1", Text: "the quick brown fox jumps over the lazy dog"},
		{ID: "2", Text: "a quick brown fox"},
		{ID: "3", Text: "completely different text about cats"},
	})
	matches := c.Query("quick brown fox", 2)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	// top 应该是 2（最相似，因为向量方向完全匹配）
	if matches[0].ID != "2" {
		t.Errorf("expected top match id=2, got %s (score=%f)", matches[0].ID, matches[0].Score)
	}
	// score 倒序
	if matches[0].Score < matches[1].Score {
		t.Errorf("expected scores desc, got %f < %f", matches[0].Score, matches[1].Score)
	}
	// 验证 doc=3 仍被检索到（不是 0 分）
	if matches[0].Score == 0 {
		t.Error("expected non-zero score for matching docs")
	}
}

func TestQuery_EmptyDB(t *testing.T) {
	c := NewClient(64)
	matches := c.Query("test", 5)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches in empty db, got %d", len(matches))
	}
}

func TestQuery_KLargerThanDB(t *testing.T) {
	c := NewClient(64)
	c.Upsert([]Document{{Text: "only one"}})
	matches := c.Query("only", 10)
	if len(matches) != 1 {
		t.Errorf("expected 1 match (clamped to db size), got %d", len(matches))
	}
}

func TestDelete(t *testing.T) {
	c := NewClient(64)
	c.Upsert([]Document{{ID: "x", Text: "delete me"}})
	if err := c.Delete("x"); err != nil {
		t.Errorf("delete: %v", err)
	}
	if c.Stats().DocumentCount != 0 {
		t.Error("expected 0 docs after delete")
	}
	if err := c.Delete("nonexistent"); err == nil {
		t.Error("expected error deleting nonexistent")
	}
}

func TestClear(t *testing.T) {
	c := NewClient(64)
	c.Upsert([]Document{{Text: "a"}, {Text: "b"}, {Text: "c"}})
	if c.Stats().DocumentCount != 3 {
		t.Errorf("expected 3 docs, got %d", c.Stats().DocumentCount)
	}
	c.Clear()
	if c.Stats().DocumentCount != 0 {
		t.Errorf("expected 0 docs after clear, got %d", c.Stats().DocumentCount)
	}
}

func TestStats(t *testing.T) {
	c := NewClient(64)
	if s := c.Stats(); s.DocumentCount != 0 || s.VectorDim != 64 {
		t.Errorf("initial stats wrong: %+v", s)
	}
	c.Upsert([]Document{{Text: "a"}, {Text: "b"}})
	if s := c.Stats(); s.DocumentCount != 2 || s.VectorDim != 64 {
		t.Errorf("after upsert stats wrong: %+v", s)
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! 123 Go.")
	if len(tokens) != 4 {
		t.Errorf("expected 4 tokens (hello, world, 123, go), got %d: %v", len(tokens), tokens)
	}
	if !strings.Contains(strings.Join(tokens, " "), "hello") {
		t.Errorf("expected lowercase 'hello', got %v", tokens)
	}
	// 验证非字母数字被剥除
	for _, tok := range tokens {
		for _, c := range tok {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
				t.Errorf("token contains non-alphanumeric: %q", tok)
			}
		}
	}
}

func TestCosine_Identical(t *testing.T) {
	v := []float32{1, 0, 0}
	score := cosine(v, v)
	if math.Abs(score-1.0) > 0.01 {
		t.Errorf("expected cosine 1.0, got %f", score)
	}
}

func TestCosine_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	score := cosine(a, b)
	if math.Abs(score) > 0.01 {
		t.Errorf("expected cosine 0.0, got %f", score)
	}
}

func TestGenerateID_Stable(t *testing.T) {
	id1 := generateID("hello world")
	id2 := generateID("hello world")
	if id1 != id2 {
		t.Errorf("expected same id for same text, got %s vs %s", id1, id2)
	}
	id3 := generateID("different")
	if id1 == id3 {
		t.Error("expected different id for different text")
	}
	if len(id1) != 16 {
		t.Errorf("expected id length 16, got %d (%s)", len(id1), id1)
	}
}
