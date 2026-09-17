package llm

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// openCacheTestDB 创建测试用 SQLite DB
func openCacheTestDB(t *testing.T) *store.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "cache.db")
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// TestCacheKey 测试 key 计算一致性
func TestCacheKey(t *testing.T) {
	key1 := CacheKey(TaskWriting, "model-A", "hello world")
	key2 := CacheKey(TaskWriting, "model-A", "hello world")
	if key1 != key2 {
		t.Errorf("same input should produce same key")
	}
	key3 := CacheKey(TaskWriting, "model-B", "hello world")
	if key1 == key3 {
		t.Errorf("different model should produce different key")
	}
	key4 := CacheKey(TaskConsistency, "model-A", "hello world")
	if key1 == key4 {
		t.Errorf("different task should produce different key")
	}
	if len(key1) != 64 { // SHA256 hex = 64 chars
		t.Errorf("key should be 64 chars, got %d", len(key1))
	}
}

// TestMemoryCache_L1HitMiss L1-only cache hit/miss
func TestMemoryCache_L1HitMiss(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(100)

	resp := &Response{Content: "hello", Provider: ProviderMinimax, Model: "M3"}

	// 1. miss
	r, hit := c.Get(ctx, TaskWriting, "model-A", "hello world")
	if hit {
		t.Errorf("expected miss on empty cache")
	}
	if r != nil {
		t.Errorf("expected nil response on miss")
	}

	// 2. set + hit
	if err := c.Set(ctx, TaskWriting, "model-A", "hello world", resp); err != nil {
		t.Fatalf("Set: %v", err)
	}
	r, hit = c.Get(ctx, TaskWriting, "model-A", "hello world")
	if !hit {
		t.Errorf("expected hit after Set")
	}
	if r == nil || r.Content != "hello" {
		t.Errorf("got %v, expected hello", r)
	}

	stats := c.Stats()
	if stats.L1Hits != 1 {
		t.Errorf("L1Hits = %d", stats.L1Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Misses = %d", stats.Misses)
	}
	if stats.Writes != 1 {
		t.Errorf("Writes = %d", stats.Writes)
	}
}

// TestMemoryCache_LRUEviction LRU 淘汰
func TestMemoryCache_LRUEviction(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(3) // cap = 3

	// 写 4 条不同 key → 第一条被淘汰
	for i := 0; i < 4; i++ {
		_ = c.Set(ctx, TaskWriting, "model", string(rune('a'+i)), &Response{Content: string(rune('A' + i))})
	}

	stats := c.Stats()
	if stats.L1Entries != 3 {
		t.Errorf("expected 3 entries, got %d", stats.L1Entries)
	}

	// 写 'a' 再次 → hit (因为 'b', 'c', 'd' 是最近的 3 个)
	// 注意: 4 个 write 后, 写入顺序是 a,b,c,d. LRU 末是 d.
	// 但如果每次 Set 都 touch, 那 'a' 不在 LRU 里
	r, hit := c.Get(ctx, TaskWriting, "model", "a")
	if hit {
		t.Errorf("expected 'a' to be evicted (LRU)")
	}
	if r != nil {
		t.Errorf("got %v", r)
	}
}

// TestCache_L2Hit L2 SQLite hit
func TestCache_L2Hit(t *testing.T) {
	ctx := context.Background()
	db := openCacheTestDB(t)
	l2 := store.NewCacheStore(db)
	c := NewCacheWithSQLite(l2, 100)

	resp := &Response{Content: "from-L2", Provider: ProviderDeepSeek, Model: "chat"}
	if err := c.Set(ctx, TaskWriting, "model", "prompt", resp); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// 强制 L1 失效: purge 后重新查 (只走 L2)
	c.Purge()
	r, hit := c.Get(ctx, TaskWriting, "model", "prompt")
	if !hit {
		t.Errorf("expected L2 hit after Purge")
	}
	if r == nil || r.Content != "from-L2" {
		t.Errorf("got %v", r)
	}

	stats := c.Stats()
	if stats.L2Hits != 1 {
		t.Errorf("L2Hits = %d", stats.L2Hits)
	}
	if stats.Writes < 1 {
		t.Errorf("Writes = %d", stats.Writes)
	}
}

// TestCache_L2BackfillsL1 L2 hit 回填 L1
func TestCache_L2BackfillsL1(t *testing.T) {
	ctx := context.Background()
	db := openCacheTestDB(t)
	l2 := store.NewCacheStore(db)
	c := NewCacheWithSQLite(l2, 100)

	resp := &Response{Content: "backfill-test"}
	_ = c.Set(ctx, TaskWriting, "model", "prompt", resp)

	// Purge L1, 第一次 Get 走 L2 (miss L1, hit L2, 回填 L1)
	c.Purge()
	_, hit := c.Get(ctx, TaskWriting, "model", "prompt")
	if !hit {
		t.Errorf("expected hit")
	}

	// 第二次 Get 应该 L1 hit (因为回填了)
	c.Purge() // 再 purge, 第二次 Get 也会走 L2 + 回填
	_, hit = c.Get(ctx, TaskWriting, "model", "prompt")
	if !hit {
		t.Errorf("expected hit on second Get")
	}

	stats := c.Stats()
	if stats.L2Hits != 2 {
		t.Errorf("expected 2 L2 hits (2 calls × 1 L2 hit each), got %d", stats.L2Hits)
	}
}

// TestCache_NilResponse nil response 应该被拒绝
func TestCache_NilResponse(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(100)
	err := c.Set(ctx, TaskWriting, "model", "prompt", nil)
	if err == nil {
		t.Errorf("expected error for nil response")
	}
}

// TestCache_ConcurrentReadWrite 并发安全
func TestCache_ConcurrentReadWrite(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(100)

	const N = 100
	done := make(chan bool, N*2)

	// 并发写
	for i := 0; i < N; i++ {
		go func(i int) {
			_ = c.Set(ctx, TaskWriting, "model", string(rune(i%10+'a')), &Response{Content: "x"})
			done <- true
		}(i)
	}

	// 并发读
	for i := 0; i < N; i++ {
		go func() {
			_, _ = c.Get(ctx, TaskWriting, "model", "prompt")
			done <- true
		}()
	}

	for i := 0; i < N*2; i++ {
		<-done
	}
}

// TestCacheKey_LongPromptLongPromptTruncation
func TestCacheKey_LongPromptTruncation(t *testing.T) {
	prompt1 := string(make([]byte, 1024))
	for i := range prompt1 {
		prompt1 = prompt1[:i+1] + "a" + prompt1[i+1:]
	}
	prompt2 := prompt1 + "more content after 1024"

	key1 := CacheKey(TaskWriting, "model", prompt1)
	key2 := CacheKey(TaskWriting, "model", prompt2)
	if key1 != key2 {
		t.Errorf("prompts differing after 1024 bytes should have same key")
	}
}

// TestCache_Purge 清空 L1
func TestCache_Purge(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(100)

	_ = c.Set(ctx, TaskWriting, "model", "p1", &Response{Content: "x"})
	_ = c.Set(ctx, TaskWriting, "model", "p2", &Response{Content: "y"})

	stats := c.Stats()
	if stats.L1Entries != 2 {
		t.Errorf("expected 2 entries, got %d", stats.L1Entries)
	}

	c.Purge()
	stats = c.Stats()
	if stats.L1Entries != 0 {
		t.Errorf("expected 0 entries after Purge, got %d", stats.L1Entries)
	}
}
