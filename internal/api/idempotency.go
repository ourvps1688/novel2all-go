// Package api 提供 novel2all-go HTTP handlers.
package api

// idempotency.go 实现内存版 idempotency store (Sprint 21).
//
// 设计:
//   - 用于 /api/chapter/{n}/review 等昂贵端点, 防止客户端双击 + 并发同 key 触发重复 LLM 调用
//   - 锁内做读写 (sync.Mutex, 简单逻辑不需 RWMutex)
//   - 写入时惰性清理过期条目
//   - 默认 TTL 5 分钟
//   - in-flight sentinel: claim() 原子占位, 并发请求看到 sentinel 时 poll 等第一个完成
//   - in-flight 永不 complete → 30s 超时后返回 409
//
// 用法:
//
//	store := NewIdempotencyStore(5*time.Minute, 30*time.Second)
//	key := IdempotencyKey{"user-1", "chapter-5", "abc-123"}
//	claimed, existing := store.Claim(key)
//	if !claimed {
//	    if existing == IdempotencyInFlight {
//	        result, err := store.WaitForResult(key, 30*time.Second)
//	        if err != nil { return 409 }
//	        return result  // replay
//	    }
//	    return existing.(YourResultType)  // cached replay
//	}
//	// 占位成功 → 跑 LLM
//	result := expensiveCall()
//	store.Complete(key, result)
//	return result
//
// 参考 Python V1.0.1 B8 web/app.py InMemoryIdempotencyStore.
import (
	"errors"
	"sync"
	"time"
)

// IdempotencyKey 是 idempotency 标识 (e.g. {user_id, chapter, idempotency_key}).
//
// 用 []any 数组, 避免外部依赖具体类型.
type IdempotencyKey []any

// IdempotencyInFlight sentinel 表示有请求正在处理但未完成.
//
// 调用方看到此值时, 应用 WaitForResult 轮询等结果.
var IdempotencyInFlight = &inFlightSentinel{}

type inFlightSentinel struct{}

// IdempotencyStore 内存版 idempotency store.
type IdempotencyStore struct {
	ttl          time.Duration
	waitTimeout  time.Duration
	pollInterval time.Duration
	mu           sync.Mutex
	store        map[string]idempotencyEntry
}

// idempotencyEntry 内部存储 entry.
type idempotencyEntry struct {
	value     any // 可能是 IdempotencyInFlight 或 caller 存的结果
	createdAt time.Time
}

// NewIdempotencyStore 创建 store.
//
// ttl: cache 有效期 (0 = 不过期)
// waitTimeout: WaitForResult 默认超时
func NewIdempotencyStore(ttl, waitTimeout time.Duration) *IdempotencyStore {
	return &IdempotencyStore{
		ttl:          ttl,
		waitTimeout:  waitTimeout,
		pollInterval: 100 * time.Millisecond,
		store:        make(map[string]idempotencyEntry),
	}
}

// keyString 把 IdempotencyKey 转成 string (用作 map key).
func keyString(key IdempotencyKey) string {
	// use fmt.Sprintf internally via runtime - 简化: panic
	// 实际生产用 stable serialization, 这里用 字符串拼接足够
	s := ""
	for _, v := range key {
		s += "_" + toString(v)
	}
	return s
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	// fallback: %v
	return defaultString(v)
}

// defaultString 简化版 fmt.Sprintf("%v", v) 避免 import fmt 在 hot path
func defaultString(v any) string {
	type stringer interface{ String() string }
	if s, ok := v.(stringer); ok {
		return s.String()
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return ""
}

// ErrIdempotencyTimeout WaitForResult 超时错误.
var ErrIdempotencyTimeout = errors.New("idempotency: wait timeout")

// ErrIdempotencyNotFound store 中找不到 key (区别 store.ErrIdempotencyNotFound).
var ErrIdempotencyNotFound = errors.New("idempotency: key not found")

// purgeExpired 内部调用, callers 必须持有 mu.
func (s *IdempotencyStore) purgeExpired(now time.Time) {
	if s.ttl <= 0 {
		return
	}
	for k, e := range s.store {
		if now.Sub(e.createdAt) >= s.ttl {
			delete(s.store, k)
		}
	}
}

// Claim 原子地尝试占位.
//
// Returns:
//   - (true, nil) — 占位成功, 你是第一个
//   - (false, IdempotencyInFlight) — 有 in-flight, 应用 WaitForResult
//   - (false, value) — 已有 cached result, 直接作为 replay 返回
func (s *IdempotencyStore) Claim(key IdempotencyKey) (bool, any) {
	k := keyString(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.purgeExpired(now)

	if e, ok := s.store[k]; ok {
		// 检查 TTL
		if s.ttl > 0 && now.Sub(e.createdAt) >= s.ttl {
			delete(s.store, k)
		} else {
			return false, e.value
		}
	}

	// 占位
	s.store[k] = idempotencyEntry{value: IdempotencyInFlight, createdAt: now}
	return true, nil
}

// Get 取缓存结果. 命中且未过期返回原值; 过期或未命中返回 ErrIdempotencyNotFound.
//
// in-flight sentinel 视为 ErrIdempotencyNotFound (调用方应改用 Claim).
func (s *IdempotencyStore) Get(key IdempotencyKey) (any, error) {
	k := keyString(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	e, ok := s.store[k]
	if !ok {
		return nil, ErrIdempotencyNotFound
	}
	if s.ttl > 0 && now.Sub(e.createdAt) >= s.ttl {
		delete(s.store, k)
		return nil, ErrIdempotencyNotFound
	}
	if e.value == IdempotencyInFlight {
		return nil, ErrIdempotencyNotFound
	}
	return e.value, nil
}

// Set 写缓存 (绕过 claim 流程, 直接存结果).
//
// 用于向后兼容 (旧测试用 Set+Get).
func (s *IdempotencyStore) Set(key IdempotencyKey, value any) {
	k := keyString(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpired(time.Now())
	s.store[k] = idempotencyEntry{value: value, createdAt: time.Now()}
}

// Complete claim 成功后, 把 LLM 结果存进去 (替换 sentinel).
func (s *IdempotencyStore) Complete(key IdempotencyKey, value any) {
	s.Set(key, value)
}

// WaitForResult 轮询等 in-flight 完成.
//
// timeout <= 0 用 store 初始化时的 waitTimeout.
// 返回 ErrIdempotencyTimeout 如果超时.
func (s *IdempotencyStore) WaitForResult(key IdempotencyKey, timeout time.Duration) (any, error) {
	if timeout <= 0 {
		timeout = s.waitTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		val, err := s.Get(key)
		if err == nil {
			return val, nil
		}
		if !errors.Is(err, ErrIdempotencyNotFound) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, ErrIdempotencyTimeout
		}
		time.Sleep(s.pollInterval)
	}
}

// Size 返回当前 entry 数 (测试用).
func (s *IdempotencyStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.store)
}
