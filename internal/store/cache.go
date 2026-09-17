package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CacheEntry llm_cache 表行
type CacheEntry struct {
	Key        string    `json:"key"`         // SHA256(prompt + model + task)
	PromptHash string    `json:"prompt_hash"` // 冗余存 SHA256(prompt) 便于 debug
	Response   string    `json:"response"`    // JSON-encoded LLM Response
	Model      string    `json:"model"`
	HitCount   int64     `json:"hit_count"`
	CreatedAt  time.Time `json:"created_at"`
	LastHitAt  time.Time `json:"last_hit_at"`
}

// CacheStore llm_cache 表 CRUD
type CacheStore struct {
	db *DB
}

// NewCacheStore 创建
func NewCacheStore(db *DB) *CacheStore {
	return &CacheStore{db: db}
}

// Get 取单条（按 key）
func (s *CacheStore) Get(ctx context.Context, key string) (*CacheEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT key, prompt_hash, response, model, hit_count, created_at, last_hit_at
		FROM llm_cache
		WHERE key = ?
	`, key)
	e, err := scanCacheEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query cache %q: %w", key, err)
	}
	return e, nil
}

// Upsert 插入或替换 response（model 变了也更新）
//
// hit_count 自动 +1，last_hit_at 自动更新为 now
func (s *CacheStore) Upsert(ctx context.Context, key, promptHash, response, model string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO llm_cache (key, prompt_hash, response, model, hit_count, created_at, last_hit_at)
		VALUES (?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			response = excluded.response,
			model = excluded.model,
			hit_count = llm_cache.hit_count + 1,
			last_hit_at = excluded.last_hit_at
	`, key, promptHash, response, model, now, now)
	if err != nil {
		return fmt.Errorf("upsert cache: %w", err)
	}
	return nil
}

// IncrementHit 命中缓存时调用（不更新 response，只 hit_count+1 + last_hit_at）
func (s *CacheStore) IncrementHit(ctx context.Context, key string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE llm_cache SET hit_count = hit_count + 1, last_hit_at = ? WHERE key = ?
	`, now, key)
	if err != nil {
		return fmt.Errorf("increment cache hit: %w", err)
	}
	return nil
}

// Delete 删除单条
func (s *CacheStore) Delete(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM llm_cache WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete cache: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeOlderThan 删除超过 maxAge 的旧缓存（按 last_hit_at）
//
// 返回删除的条数
func (s *CacheStore) PurgeOlderThan(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	res, err := s.db.ExecContext(ctx, `DELETE FROM llm_cache WHERE last_hit_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge cache: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Stats 缓存统计
type CacheStats struct {
	TotalEntries int64
	TotalHits    int64
	UniqueModels int
}

// Stats 返回缓存统计
func (s *CacheStore) Stats(ctx context.Context) (CacheStats, error) {
	var stats CacheStats
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(hit_count), 0), COUNT(DISTINCT model)
		FROM llm_cache
	`)
	err := row.Scan(&stats.TotalEntries, &stats.TotalHits, &stats.UniqueModels)
	if err != nil {
		return stats, fmt.Errorf("cache stats: %w", err)
	}
	return stats, nil
}

// ListByModel 按 model 列缓存（用于 debug / metrics）
func (s *CacheStore) ListByModel(ctx context.Context, model string) ([]*CacheEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, prompt_hash, response, model, hit_count, created_at, last_hit_at
		FROM llm_cache
		WHERE model = ?
		ORDER BY last_hit_at DESC
	`, model)
	if err != nil {
		return nil, fmt.Errorf("list cache by model: %w", err)
	}
	defer rows.Close()
	return scanCacheEntries(rows)
}

func scanCacheEntries(rows *sql.Rows) ([]*CacheEntry, error) {
	out := make([]*CacheEntry, 0)
	for rows.Next() {
		e, err := scanCacheEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanCacheEntry(s rowScanner) (*CacheEntry, error) {
	var e CacheEntry
	var createdAt, lastHitAt string
	err := s.Scan(&e.Key, &e.PromptHash, &e.Response, &e.Model, &e.HitCount, &createdAt, &lastHitAt)
	if err != nil {
		return nil, err
	}
	e.CreatedAt = parseTime(createdAt)
	e.LastHitAt = parseTime(lastHitAt)
	return &e, nil
}
