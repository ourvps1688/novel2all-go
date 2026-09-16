// Package auth 提供 novel2all-go 的认证与限流能力。
//
// 包含：
//   - bcrypt 密码 hash/verify
//   - Session 管理（cookie + DB 持久化）
//   - 登录失败限流（5 分钟 5 次 → 锁定 5 分钟）
//   - RBAC（admin / user）
//
// 设计目标：与 Python V1.5.x 的 session auth 完全兼容（用户配置不变）。
package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost 默认 cost（与 Python passlib default=12 不同，Go 用 10 平衡速度）
const BcryptCost = 10

// ErrPasswordMismatch 密码不匹配
var ErrPasswordMismatch = errors.New("password mismatch")

// HashPassword 用 bcrypt 哈希密码
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("empty password")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}
	return string(h), nil
}

// VerifyPassword 验证密码与 hash 是否匹配
func VerifyPassword(hash, plain string) error {
	if hash == "" || plain == "" {
		return ErrPasswordMismatch
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrPasswordMismatch
		}
		return fmt.Errorf("bcrypt compare: %w", err)
	}
	return nil
}
