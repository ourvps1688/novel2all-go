package auth

import (
	"testing"
	"time"
)

func TestRateLimiter_Lockout(t *testing.T) {
	rl := NewRateLimiter(3, 1*time.Minute, 30*time.Second)

	// 前 2 次失败不锁定
	for i := 0; i < 2; i++ {
		locked, _ := rl.Check("test")
		if locked {
			t.Errorf("第 %d 次失败不应锁定", i+1)
		}
		newLock, _ := rl.RecordFailure("test")
		if newLock {
			t.Errorf("第 %d 次失败不应触发新锁定", i+1)
		}
	}

	// 第 3 次失败触发锁定
	locked, _ := rl.Check("test")
	if locked {
		t.Error("第 3 次失败前 Check 应未锁定")
	}
	newLock, ra := rl.RecordFailure("test")
	if !newLock {
		t.Error("第 3 次失败应触发新锁定")
	}
	if ra != 30*time.Second {
		t.Errorf("retryAfter 应为 30s，实际=%v", ra)
	}
	if ra == 0 {
		t.Error("retryAfter 不应为 0")
	}

	// 现在应该被锁
	locked, _ = rl.Check("test")
	if !locked {
		t.Error("第 3 次失败后应被锁")
	}
}

func TestRateLimiter_RecordSuccess_Resets(t *testing.T) {
	rl := NewRateLimiter(3, 1*time.Minute, 30*time.Second)

	// 失败 2 次
	rl.RecordFailure("test")
	rl.RecordFailure("test")

	// 成功 → 重置
	rl.RecordSuccess("test")

	// 再失败 3 次不应该立刻锁（因为计数已重置）
	rl.RecordFailure("test")
	rl.RecordFailure("test")
	newLock, _ := rl.RecordFailure("test")
	if !newLock {
		t.Error("第 3 次失败应该触发锁定（计数重置后）")
	}
}

func TestRateLimiter_DifferentKeys(t *testing.T) {
	rl := NewRateLimiter(2, 1*time.Minute, 30*time.Second)

	// user1 失败 2 次
	rl.RecordFailure("user1")
	rl.RecordFailure("user1")
	// user2 失败 1 次（不应被 user1 影响）
	newLock, _ := rl.RecordFailure("user2")
	if newLock {
		t.Error("user2 第 1 次失败不应触发锁定")
	}
}
