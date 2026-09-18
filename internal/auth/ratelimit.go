// Package auth 提供用户认证、session 管理、密码哈希、限流.
//
// ratelimit.go 定义 Limiter 接口 + 内存版 RateLimiter 实现.
// Redis 版实现见 ratelimit_redis.go (Sprint V1.0.1 P5).
//
// 设计:
//   - Limiter interface 抽象出 auth handler 需要的 4 个方法
//   - 内存版 (RateLimiter): 进程级, 重启清空, 适合单进程部署
//   - Redis 版 (RedisLimiter): 跨进程/跨实例共享, 适合多副本部署
//   - 选择由 server.Run 根据 REDIS_URL 环境变量自动切换
package auth

import (
	"errors"
	"sync"
	"time"
)

// ErrLimiterUnavailable limiter backend 暂时不可用 (Redis 断连/超时).
//
// 调用方应 fail-open 或 fallback 到其他 limiter. 不是 fatal error.
var ErrLimiterUnavailable = errors.New("limiter backend unavailable")

// Limiter 限流器接口 (Sprint V1.0.1 P5 抽象).
//
// 实现:
//   - RateLimiter (本文件): 内存版, 单进程
//   - RedisLimiter (ratelimit_redis.go): Redis 版, 多进程共享
//
// auth handler 调用顺序:
//
//	locked, _ := limiter.Check(key)            // 入口查锁
//	... 业务逻辑 ...
//	limiter.RecordFailure(key)                 // 失败累加
//	limiter.RecordSuccess(key)                 // 成功清零
//
// 多 goroutine 并发安全 (所有方法需内部加锁).
type Limiter interface {
	// Check 检查 key (IP/username) 是否被锁定.
	// 返回 (locked, retryAfter). locked=true 时不应放行.
	Check(key string) (bool, time.Duration)

	// RecordFailure 记录一次失败, 触发锁定时返回 (newLock=true, retryAfter).
	// 不锁定时返回 (false, 0).
	RecordFailure(key string) (newLock bool, retryAfter time.Duration)

	// RecordSuccess 记录成功, 清空该 key 的失败记录.
	RecordSuccess(key string)

	// Cleanup 清理过期记录 (可选, 内存版可用, Redis 版一般交给 Redis TTL).
	Cleanup()
}

// Compile-time 断言: RateLimiter 实现 Limiter 接口.
var _ Limiter = (*RateLimiter)(nil)

// RateLimiter 简单的失败次数限流器（基于内存，进程级）
//
// 设计：5 分钟内 5 次失败 → 锁定 5 分钟
// 这与 Python V1.5.x 行为一致。
//
// 注意：
//   - 单进程内存，重启清空（可接受）
//   - 多实例部署需要 Redis 或共享存储（生产 Sprint V1.0.1 P5 加 RedisLimiter）
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

// NewRateLimiter 创建内存版限流器.
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

// Check 检查 key (IP 或 username) 是否被锁定.
// 返回 (locked, retryAfter).
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

// RecordFailure 记录一次失败.
// 触发锁定条件时返回 (newLock=true, retryAfter).
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

// RecordSuccess 记录成功 (清空失败计数).
func (r *RateLimiter) RecordSuccess(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failures, key)
}

// Cleanup 清理过期的记录 (可定期调).
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
