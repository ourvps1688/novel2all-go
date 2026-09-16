package auth

import (
	"sync"
	"time"
)

// RateLimiter 简单的失败次数限流器（基于内存，进程级）
//
// 设计：5 分钟内 5 次失败 → 锁定 5 分钟
// 这与 Python V1.5.x 行为一致。
//
// 注意：
//   - 单进程内存，重启清空（可接受）
//   - 多实例部署需要 Redis 或共享存储（生产 P2 阶段再加）
type RateLimiter struct {
	mu          sync.Mutex
	failures    map[string]*failureRecord
	limit       int
	window      time.Duration
	lockoutTime time.Duration
}

type failureRecord struct {
	count     int
	firstAt   time.Time
	lockedAt  time.Time
	lockedTil time.Time
}

// NewRateLimiter 创建限流器
//
// limit: 窗口内允许的最大失败次数
// window: 计数窗口
// lockoutTime: 锁定时长
func NewRateLimiter(limit int, window, lockoutTime time.Duration) *RateLimiter {
	return &RateLimiter{
		failures:    make(map[string]*failureRecord),
		limit:       limit,
		window:      window,
		lockoutTime: lockoutTime,
	}
}

// Check 检查 key（IP 或 username）是否被锁定
// 返回 (locked, retryAfter)
func (r *RateLimiter) Check(key string) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, ok := r.failures[key]
	if !ok {
		return false, 0
	}
	now := time.Now()

	// 还在锁定中？
	if !rec.lockedTil.IsZero() && now.Before(rec.lockedTil) {
		return true, rec.lockedTil.Sub(now)
	}

	// 锁定已过 → 重置
	if !rec.lockedTil.IsZero() && now.After(rec.lockedTil) {
		delete(r.failures, key)
		return false, 0
	}

	// 计数窗口已过 → 重置计数
	if now.Sub(rec.firstAt) > r.window {
		delete(r.failures, key)
		return false, 0
	}

	return false, 0
}

// RecordFailure 记录一次失败
// 触发锁定条件时返回 true（表示新触发锁定）
func (r *RateLimiter) RecordFailure(key string) (newLock bool, retryAfter time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	rec, ok := r.failures[key]
	if !ok {
		r.failures[key] = &failureRecord{
			count:   1,
			firstAt: now,
		}
		return false, 0
	}

	// 锁定已过 → 重置
	if !rec.lockedTil.IsZero() && now.After(rec.lockedTil) {
		r.failures[key] = &failureRecord{
			count:   1,
			firstAt: now,
		}
		return false, 0
	}

	// 窗口已过 → 重置计数
	if now.Sub(rec.firstAt) > r.window {
		rec.count = 1
		rec.firstAt = now
		rec.lockedAt = time.Time{}
		rec.lockedTil = time.Time{}
		return false, 0
	}

	// 累加
	rec.count++
	if rec.count >= r.limit {
		rec.lockedAt = now
		rec.lockedTil = now.Add(r.lockoutTime)
		return true, r.lockoutTime
	}

	return false, 0
}

// RecordSuccess 记录成功（清空失败计数）
func (r *RateLimiter) RecordSuccess(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failures, key)
}

// Cleanup 清理过期的记录（可定期调）
func (r *RateLimiter) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for k, rec := range r.failures {
		if !rec.lockedTil.IsZero() && now.After(rec.lockedTil) {
			delete(r.failures, k)
		} else if now.Sub(rec.firstAt) > r.window {
			delete(r.failures, k)
		}
	}
}
