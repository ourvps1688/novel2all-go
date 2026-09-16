package auth

import (
	"errors"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	plain := "mysecret123"
	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == plain {
		t.Error("hash 不应等于原密码")
	}
	if len(hash) < 50 {
		t.Errorf("bcrypt hash 太短：%d 字符", len(hash))
	}

	// 正确密码
	if err := VerifyPassword(hash, plain); err != nil {
		t.Errorf("正确密码应通过：%v", err)
	}

	// 错误密码
	if err := VerifyPassword(hash, "wrongpass"); !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("错误密码应返回 ErrPasswordMismatch，实际=%v", err)
	}
}

func TestEmptyPassword(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Error("空密码应报错")
	}
	if err := VerifyPassword("hash", ""); !errors.Is(err, ErrPasswordMismatch) {
		t.Error("空密码应返回 ErrPasswordMismatch")
	}
}
