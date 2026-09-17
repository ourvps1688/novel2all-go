// Package store: cache_migrate.go — Sprint 27 cache 迁移工具.
//
// 对齐 Python V1 core/migration.py: migrate_cache() + MigrationResult.
//
// 支持 cache backend 之间平滑迁移 (零数据丢失).
// 当前实现: json ↔ sqlite (最常见场景).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// ProgressCallback 进度回调 (migrated, total).
type ProgressCallback func(done, total int)

// MigrationResult 迁移结果统计.
type MigrationResult struct {
	SrcBackend     string   `json:"src_backend"`
	DstBackend     string   `json:"dst_backend"`
	SrcPath        string   `json:"src_path"`
	DstPath        string   `json:"dst_path"`
	TotalEntries   int      `json:"total_entries"`
	Migrated       int      `json:"migrated"`
	SkippedExpired int      `json:"skipped_expired"`
	Errors         []string `json:"errors,omitempty"`
	ElapsedSeconds float64  `json:"elapsed_seconds"`
}

// Summary 人类可读摘要.
func (r *MigrationResult) Summary() string {
	return fmt.Sprintf(
		"Migration %s→%s 完成:\n"+
			"  - total: %d\n"+
			"  - migrated: %d\n"+
			"  - skipped_expired: %d\n"+
			"  - errors: %d\n"+
			"  - elapsed: %.2fs",
		r.SrcBackend, r.DstBackend,
		r.TotalEntries, r.Migrated, r.SkippedExpired,
		len(r.Errors), r.ElapsedSeconds,
	)
}

// Cache backend 名称常量 (用于 srcBackend/dstBackend 参数).
const (
	backendJSON   = "json"
	backendSQLite = "sqlite"
	backendMemory = "memory"
)

// Cacheable 通用 cache 抽象 (migrate 用).
//
// CacheStore (SQLite) 和 JSONCache 都实现这个接口, 这样 migrate 可跨 backend.
type Cacheable interface {
	Get(key string) interface{}
	Set(key string, value interface{}) error
	Keys() []string
	Size() int
	Close() error
}

// MigrateCache 从 src 迁移所有 entries 到 dst.
//
// 参数:
//
//	srcBackend, dstBackend: "json" | "sqlite"
//	srcPath, dstPath:       文件路径
//	maxSize, ttlSeconds:    目标 cache 配置
//	progressCb:             进度回调 (可选)
//
// 注意: 不修改源 cache (只读). 目标 cache 已存在则覆盖.
func MigrateCache(srcBackend, dstBackend, srcPath, dstPath string,
	maxSize, ttlSeconds int, progressCb ProgressCallback) (*MigrationResult, error) {

	start := time.Now()
	result := &MigrationResult{
		SrcBackend: srcBackend,
		DstBackend: dstBackend,
		SrcPath:    srcPath,
		DstPath:    dstPath,
		Errors:     []string{},
	}

	if srcBackend != backendJSON && srcBackend != backendSQLite {
		return result, fmt.Errorf("unsupported src backend %q (json|sqlite)", srcBackend)
	}
	if dstBackend != backendJSON && dstBackend != backendSQLite {
		return result, fmt.Errorf("unsupported dst backend %q (json|sqlite)", dstBackend)
	}

	// 1. 打开 src + dst
	src, err := openCacheBackend(srcBackend, srcPath, maxSize, ttlSeconds)
	if err != nil {
		return result, fmt.Errorf("open src: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := openCacheBackend(dstBackend, dstPath, maxSize, ttlSeconds)
	if err != nil {
		return result, fmt.Errorf("open dst: %w", err)
	}
	defer func() { _ = dst.Close() }()

	// 2. 收集 src entries
	result.TotalEntries = src.Size()
	if result.TotalEntries == 0 {
		result.ElapsedSeconds = time.Since(start).Seconds()
		return result, nil
	}

	// 3. 读 + 写
	keys := src.Keys()
	for _, key := range keys {
		value := src.Get(key)
		if value == nil {
			result.SkippedExpired++
			continue
		}
		if err := dst.Set(key, value); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("key=%q: %v", key, err))
			continue
		}
		result.Migrated++

		if progressCb != nil && result.Migrated%50 == 0 {
			progressCb(result.Migrated, result.TotalEntries)
		}
	}

	// 4. 最终回调
	if progressCb != nil {
		progressCb(result.Migrated, result.TotalEntries)
	}

	result.ElapsedSeconds = time.Since(start).Seconds()
	return result, nil
}

// openCacheBackend 构造 cache backend (json/sqlite).
func openCacheBackend(backend, path string, maxSize, ttlSeconds int) (Cacheable, error) {
	if backend == backendMemory {
		return nil, fmt.Errorf("memory backend not supported for migration")
	}
	switch backend {
	case backendJSON:
		return NewJSONCache(path, maxSize, ttlSeconds)
	case backendSQLite:
		return NewSQLiteCache(path, maxSize, ttlSeconds)
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
}

// ============================================================
// JSON backend (Sprint 27)
// ============================================================

// JSONCache 文件 JSON cache (map[string]interface{} 持久化).
type JSONCache struct {
	mu         sync.RWMutex
	path       string
	maxSize    int
	ttlSeconds int
	data       map[string]jsonCacheItem
}

type jsonCacheItem struct {
	Value     interface{} `json:"value"`
	ExpiresAt int64       `json:"expires_at,omitempty"` // unix nano; 0 = never
}

// NewJSONCache 构造 (不存在则创建, 存在则加载).
func NewJSONCache(path string, maxSize, ttlSeconds int) (*JSONCache, error) {
	c := &JSONCache{
		path:       path,
		maxSize:    maxSize,
		ttlSeconds: ttlSeconds,
		data:       make(map[string]jsonCacheItem),
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

// load 从文件加载 (atomic write via tmp+rename).
func (c *JSONCache) load() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在 = 空 cache
		}
		return fmt.Errorf("read json cache: %w", err)
	}
	return json.Unmarshal(data, &c.data)
}

// save 持久化 (tmp + rename atomic).
func (c *JSONCache) save() error {
	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	return os.Rename(tmp, c.path)
}

func (c *JSONCache) isExpired(item jsonCacheItem) bool {
	if item.ExpiresAt == 0 {
		return false
	}
	return time.Now().UnixNano() > item.ExpiresAt
}

// Get 取值 (nil 表示 not-found 或 expired).
func (c *JSONCache) Get(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.data[key]
	if !ok {
		return nil
	}
	if c.isExpired(item) {
		return nil
	}
	return item.Value
}

// Set 存值 (TTL 0 = 不过期).
func (c *JSONCache) Set(key string, value interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// LRU 淘汰
	if c.maxSize > 0 && len(c.data) >= c.maxSize {
		if _, exists := c.data[key]; !exists {
			// 简单淘汰: 删除第一个 key (无 LRU 时序)
			for k := range c.data {
				delete(c.data, k)
				break
			}
		}
	}
	var expiresAt int64
	if c.ttlSeconds > 0 {
		expiresAt = time.Now().Add(time.Duration(c.ttlSeconds) * time.Second).UnixNano()
	}
	c.data[key] = jsonCacheItem{Value: value, ExpiresAt: expiresAt}
	return c.save()
}

// Keys 返回所有未过期 keys.
func (c *JSONCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now := time.Now().UnixNano()
	keys := make([]string, 0, len(c.data))
	for k, item := range c.data {
		if item.ExpiresAt == 0 || item.ExpiresAt > now {
			keys = append(keys, k)
		}
	}
	return keys
}

// Size 返回当前 entry 数.
func (c *JSONCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Close 释放资源 (JSON 无 op).
func (c *JSONCache) Close() error {
	return nil
}
