package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Session 表示一行 sessions 表
type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
	IP        string
	UserAgent string
	CreatedAt time.Time
}

// ErrSessionNotFound session 不存在
var ErrSessionNotFound = errors.New("session not found")

// CreateSession 创建新 session（生成随机 token，存到 DB）
func (db *DB) CreateSession(ctx context.Context, userID int64, ip, userAgent string, ttl time.Duration) (string, error) {
	token, err := generateToken(32) // 64 字符 hex
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	expiresAt := time.Now().UTC().Add(ttl)

	_, err = db.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, ip, user_agent)
		VALUES (?, ?, ?, ?, ?)
	`, token, userID, expiresAt.Format("2006-01-02 15:04:05"), ip, userAgent)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return token, nil
}

// GetSession 按 token 查 session（含过期检查）
func (db *DB) GetSession(ctx context.Context, token string) (*Session, error) {
	row := db.QueryRowContext(ctx, `
		SELECT token, user_id, expires_at, ip, user_agent, created_at
		FROM sessions
		WHERE token = ?
	`, token)

	var s Session
	var expiresAt, createdAt string
	if err := row.Scan(&s.Token, &s.UserID, &expiresAt, &s.IP, &s.UserAgent, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	parsedExpires, _ := time.Parse("2006-01-02 15:04:05", expiresAt)
	parsedCreated, _ := time.Parse("2006-01-02 15:04:05", createdAt)
	s.ExpiresAt = parsedExpires
	s.CreatedAt = parsedCreated

	// 过期检查
	if !s.ExpiresAt.IsZero() && time.Now().After(s.ExpiresAt) {
		// 异步清理过期 session（不阻塞返回）
		go func(token string) {
			_ = db.DeleteSession(context.Background(), token)
		}(token)
		return nil, ErrSessionNotFound
	}

	return &s, nil
}

// DeleteSession 删 session
func (db *DB) DeleteSession(ctx context.Context, token string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions 删用户所有 session（用于禁用账号时清空所有登录）
func (db *DB) DeleteUserSessions(ctx context.Context, userID int64) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("delete user sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// PurgeExpiredSessions 清过期 session（可定期调）
func (db *DB) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < datetime('now')`)
	if err != nil {
		return 0, fmt.Errorf("purge expired: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// generateToken 生成随机 hex token
func generateToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
