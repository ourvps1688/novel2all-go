package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// CacheEntry L1 内存 cache 的条目
type CacheEntry struct {
	Response *Response
	StoredAt time.Time
}

// CacheStats cache 命中率统计
type CacheStats struct {
	L1Entries int   // L1 当前条目数
	L1Hits    int64 // L1 命中次数
	L2Hits    int64 // L2 (SQLite) 命中次数
	Misses    int64 // 双层都未命中次数
	Writes    int64 // 写入次数（双层都算）
}

// Cache L1 内存 + L2 SQLite 双层 cache
//
// 设计：
//   - L1: sync.Map + LRU 淘汰（容量默认 1024）
//   - L2: store.CacheStore (SQLite llm_cache 表)
//   - Key: SHA256(task + "|" + model + "|" + prompt[:1024])
//   - 读：L1 命中 → 立即返回；否则查 L2；命中后回填 L1
//   - 写：同步写 L1 + 异步写 L2 (失败不影响读返回)
//
// 为何 L1 + L2：
//   - L1 内存避免每次 chat 走 SQLite IO（同 prompt 重发常见）
//   - L2 持久化保证重启后 cache 仍有效（节省 token 成本）
type Cache struct {
	l1      sync.Map          // key string → *CacheEntry
	l1Cap   int               // max L1 entries
	l1Order []string          // LRU 顺序（最近访问在尾部）
	l1Mu    sync.Mutex        // 保护 l1Order
	l2      *store.CacheStore // SQLite（可选，nil = L2 disabled）

	// Metrics
	hitsL1 atomic.Int64
	hitsL2 atomic.Int64
	misses atomic.Int64
	writes atomic.Int64
}

// NewMemoryCache 创建 L1-only 内存 cache（无 L2 SQLite）
//
// 适用于：测试 / 单进程 / 无 SQLite 部署
func NewMemoryCache(maxEntries int) *Cache {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	return &Cache{l1Cap: maxEntries}
}

// NewCacheWithSQLite 创建 L1 内存 + L2 SQLite 双层
//
// l2 可为 nil（disable L2）
func NewCacheWithSQLite(l2 *store.CacheStore, maxEntries int) *Cache {
	c := NewMemoryCache(maxEntries)
	c.l2 = l2
	return c
}

// CacheKey 计算 cache key
//
// 包含 task + model + prompt 前 1024 字节（避免 SHA256 过长 prompt）
func CacheKey(task TaskType, model, prompt string) string {
	h := sha256.New()
	h.Write([]byte(string(task)))
	h.Write([]byte{'|'})
	h.Write([]byte(model))
	h.Write([]byte{'|'})
	if len(prompt) > 1024 {
		h.Write([]byte(prompt[:1024]))
	} else {
		h.Write([]byte(prompt))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Get 查 cache（先 L1 → 后 L2）
//
// 返回 (response, true) 表示命中；(nil, false) 表示未命中。
// L2 命中会自动回填 L1。
func (c *Cache) Get(ctx context.Context, task TaskType, model, prompt string) (*Response, bool) {
	key := CacheKey(task, model, prompt)

	// L1 lookup
	if v, ok := c.l1.Load(key); ok {
		//nolint:errcheck // type assertion OK (CacheEntry 是唯一写入类型)
		entry := v.(*CacheEntry)
		// 移到 LRU 末尾
		c.touch(key)
		c.hitsL1.Add(1)
		return entry.Response, true
	}

	// L2 lookup (SQLite)
	if c.l2 == nil {
		c.misses.Add(1)
		return nil, false
	}

	entry, err := c.l2.Get(ctx, key)
	if err != nil {
		c.misses.Add(1)
		return nil, false
	}

	// L2 hit - 反序列化 + 回填 L1
	resp, err := unmarshalResponse(entry.Response)
	if err != nil {
		// JSON 损坏 — 删掉 + miss
		_ = c.l2.Delete(ctx, key)
		c.misses.Add(1)
		return nil, false
	}

	c.putL1(key, resp)
	_ = c.l2.IncrementHit(ctx, key)
	c.hitsL2.Add(1)
	return resp, true
}

// Set 写入 cache（L1 + L2）
//
// L2 写入错误不返回（cache 写入不影响 Chat 结果）
func (c *Cache) Set(ctx context.Context, task TaskType, model, prompt string, resp *Response) error {
	if resp == nil {
		return fmt.Errorf("nil response")
	}
	key := CacheKey(task, model, prompt)

	// L1 同步写
	c.putL1(key, resp)
	c.writes.Add(1)

	// L2 异步写（错误仅记录，不返回）
	if c.l2 != nil {
		body, err := marshalResponse(resp)
		if err != nil {
			return fmt.Errorf("marshal response: %w", err)
		}
		if err := c.l2.Upsert(ctx, key, "", body, model); err != nil {
			return fmt.Errorf("L2 upsert: %w", err)
		}
	}

	return nil
}

// Stats 返回 cache 命中率
func (c *Cache) Stats() CacheStats {
	c.l1Mu.Lock()
	size := len(c.l1Order)
	c.l1Mu.Unlock()
	return CacheStats{
		L1Entries: size,
		L1Hits:    c.hitsL1.Load(),
		L2Hits:    c.hitsL2.Load(),
		Misses:    c.misses.Load(),
		Writes:    c.writes.Load(),
	}
}

// Purge 清空 L1（不删 L2）
func (c *Cache) Purge() {
	c.l1.Range(func(k, _ any) bool {
		c.l1.Delete(k)
		return true
	})
	c.l1Mu.Lock()
	c.l1Order = c.l1Order[:0]
	c.l1Mu.Unlock()
}

// putL1 写入 L1 (LRU 淘汰)
func (c *Cache) putL1(key string, resp *Response) {
	c.l1Mu.Lock()
	defer c.l1Mu.Unlock()

	// 如果已存在, 删除旧位置
	for i, k := range c.l1Order {
		if k == key {
			c.l1Order = append(c.l1Order[:i], c.l1Order[i+1:]...)
			break
		}
	}
	// 加到末尾
	c.l1Order = append(c.l1Order, key)

	// LRU 淘汰（如果超过 cap）
	for len(c.l1Order) > c.l1Cap {
		oldest := c.l1Order[0]
		c.l1Order = c.l1Order[1:]
		c.l1.Delete(oldest)
	}

	c.l1.Store(key, &CacheEntry{
		Response: resp,
		StoredAt: time.Now(),
	})
}

// touch 把 key 移到 LRU 末尾（命中时调用）
func (c *Cache) touch(key string) {
	c.l1Mu.Lock()
	defer c.l1Mu.Unlock()
	for i, k := range c.l1Order {
		if k == key {
			c.l1Order = append(append(c.l1Order[:i], c.l1Order[i+1:]...), key)
			return
		}
	}
}

// Snapshot 返回 L1 keys（按 LRU 顺序，debug 用）
func (c *Cache) SnapshotKeys() []string {
	c.l1Mu.Lock()
	defer c.l1Mu.Unlock()
	out := make([]string, len(c.l1Order))
	copy(out, c.l1Order)
	sort.Strings(out) // 排序方便测试
	return out
}

// marshalResponse Response → JSON string (存 L2)
func marshalResponse(r *Response) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalResponse JSON string → *Response (从 L2 读)
func unmarshalResponse(s string) (*Response, error) {
	var r Response
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, err
	}
	return &r, nil
}
