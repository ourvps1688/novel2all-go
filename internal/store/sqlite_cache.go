// Package store: sqlite_cache.go — SQLiteCache 实现 Cacheable 接口.
//
// 用于 cache_migrate (Sprint 27). 复用 CacheStore 但包装为通用 Cacheable.
package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// SQLiteCache wraps CacheStore 实现 Cacheable (用于 cache_migrate).
type SQLiteCache struct {
	store *CacheStore
}

// NewSQLiteCache 构造 (path 是 SQLite DB 文件).
//
// 注意: path 需是已存在或可创建的 SQLite 文件. 首次调用 Migrate 不会自动
// 跑 schema (需先 Open + Migrate). 这里只打开现有 DB.
func NewSQLiteCache(path string, maxSize, ttlSeconds int) (*SQLiteCache, error) {
	_ = maxSize // V0 简化: SQLite 不实现 LRU (依赖 schema)
	_ = ttlSeconds

	ctx := context.Background()
	db, err := Open(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return &SQLiteCache{store: NewCacheStore(db)}, nil
}

// Get 取值 (返回 interface{} 兼容 Cacheable; 实际返回 map).
func (c *SQLiteCache) Get(key string) interface{} {
	ctx := context.Background()
	entry, err := c.store.Get(ctx, key)
	if err != nil {
		return nil
	}
	// 反序列化 JSON response 到 map (与 JSONCache.Value 对齐)
	var val interface{}
	if err := json.Unmarshal([]byte(entry.Response), &val); err != nil {
		// 如果反序列化失败 (legacy 字符串), 直接返回字符串
		return entry.Response
	}
	return val
}

// Set 存值 (interface{} → JSON marshal → CacheStore.Upsert).
func (c *SQLiteCache) Set(key string, value interface{}) error {
	ctx := context.Background()
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}
	// SHA256(prompt + model + task) -- V0 简化: 用 key 自身
	return c.store.Upsert(ctx, key, key, string(data), "migrated")
}

// Keys 返回所有未过期 keys.
func (c *SQLiteCache) Keys() []string {
	ctx := context.Background()
	keys, err := c.store.ListAllKeys(ctx)
	if err != nil {
		return []string{}
	}
	return keys
}

// Size 返回当前 entry 数 (通过 stats 近似).
func (c *SQLiteCache) Size() int {
	ctx := context.Background()
	stats, err := c.store.Stats(ctx)
	if err != nil {
		return 0
	}
	return int(stats.TotalEntries)
}

// Close 关闭 SQLite DB.
func (c *SQLiteCache) Close() error {
	return c.store.db.Close()
}
