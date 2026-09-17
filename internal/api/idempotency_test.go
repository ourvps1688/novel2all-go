// idempotency_test.go 测试 IdempotencyStore.
package api

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestIdempotencyStore_ClaimAndComplete(t *testing.T) {
	store := NewIdempotencyStore(5*time.Minute, 100*time.Millisecond)
	key := IdempotencyKey{"user-1", "chapter-5", "abc-123"}

	// 第一次 claim → 成功
	claimed, existing := store.Claim(key)
	if !claimed {
		t.Errorf("first claim should succeed, got claimed=%v existing=%v", claimed, existing)
	}
	if existing != nil {
		t.Errorf("first claim existing should be nil, got %v", existing)
	}

	// 第二次 claim (同一 key) → 失败, 返回 in-flight sentinel
	claimed, existing = store.Claim(key)
	if claimed {
		t.Errorf("second claim should fail (in-flight)")
	}
	if existing != IdempotencyInFlight {
		t.Errorf("existing should be IdempotencyInFlight, got %T %v", existing, existing)
	}

	// Complete
	store.Complete(key, "result-1")

	// 第三次 claim → 返回 cached result
	claimed, existing = store.Claim(key)
	if claimed {
		t.Errorf("third claim should fail (cached)")
	}
	if existing != "result-1" {
		t.Errorf("existing should be result-1, got %v", existing)
	}
}

func TestIdempotencyStore_Get(t *testing.T) {
	store := NewIdempotencyStore(5*time.Minute, 100*time.Millisecond)
	key := IdempotencyKey{"user-2", "chapter-1"}

	// 未设置
	if _, err := store.Get(key); !errors.Is(err, ErrIdempotencyNotFound) {
		t.Errorf("Get unset key should return ErrIdempotencyNotFound, got %v", err)
	}

	// Set + Get
	store.Set(key, "cached")
	got, err := store.Get(key)
	if err != nil {
		t.Errorf("Get set key should not error: %v", err)
	}
	if got != "cached" {
		t.Errorf("Get returned %v, want cached", got)
	}
}

func TestIdempotencyStore_TTLExpiry(t *testing.T) {
	store := NewIdempotencyStore(50*time.Millisecond, 100*time.Millisecond)
	key := IdempotencyKey{"u", "c"}

	store.Set(key, "value")
	if _, err := store.Get(key); err != nil {
		t.Errorf("immediate Get failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := store.Get(key); !errors.Is(err, ErrIdempotencyNotFound) {
		t.Errorf("after TTL Get should return ErrIdempotencyNotFound, got %v", err)
	}
}

func TestIdempotencyStore_WaitForResult(t *testing.T) {
	store := NewIdempotencyStore(5*time.Minute, 500*time.Millisecond)
	key := IdempotencyKey{"u", "c"}

	claimed, _ := store.Claim(key)
	if !claimed {
		t.Fatal("claim should succeed")
	}

	// 后台 100ms 后 Complete
	go func() {
		time.Sleep(100 * time.Millisecond)
		store.Complete(key, "async-result")
	}()

	// WaitForResult 应该等到 result
	val, err := store.WaitForResult(key, 500*time.Millisecond)
	if err != nil {
		t.Errorf("WaitForResult error: %v", err)
	}
	if val != "async-result" {
		t.Errorf("WaitForResult = %v, want async-result", val)
	}
}

func TestIdempotencyStore_WaitForResultTimeout(t *testing.T) {
	store := NewIdempotencyStore(5*time.Minute, 100*time.Millisecond)
	key := IdempotencyKey{"u", "c"}

	claimed, _ := store.Claim(key)
	if !claimed {
		t.Fatal("claim should succeed")
	}
	// 不 Complete, 等超时

	_, err := store.WaitForResult(key, 200*time.Millisecond)
	if !errors.Is(err, ErrIdempotencyTimeout) {
		t.Errorf("WaitForResult should timeout, got %v", err)
	}
}

func TestIdempotencyStore_ConcurrentClaim(t *testing.T) {
	// 并发安全: 100 个 goroutine 同时 Claim 同一 key, 应该有且仅有 1 个成功
	store := NewIdempotencyStore(5*time.Minute, 100*time.Millisecond)
	key := IdempotencyKey{"race", "key"}

	const N = 100
	results := make([]bool, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			claimed, _ := store.Claim(key)
			results[idx] = claimed
		}(i)
	}
	wg.Wait()

	count := 0
	for _, r := range results {
		if r {
			count++
		}
	}
	if count != 1 {
		t.Errorf("concurrent Claim: %d succeeded, want 1", count)
	}
}
