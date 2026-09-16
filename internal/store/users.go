package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User 表示一行 users 表
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string // 'admin' | 'user'
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ErrUserNotFound 用户不存在
var ErrUserNotFound = errors.New("user not found")

// ErrUserExists 用户已存在
var ErrUserExists = errors.New("user already exists")

// CreateUser 创建用户
func (db *DB) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	res, err := db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, role)
		VALUES (?, ?, ?)
	`, username, passwordHash, role)
	if err != nil {
		// 唯一约束冲突
		if isUniqueConstraintError(err) {
			return 0, ErrUserExists
		}
		return 0, fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// GetUserByUsername 按用户名查
func (db *DB) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, disabled, created_at, updated_at
		FROM users
		WHERE username = ?
	`, username)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return u, nil
}

// GetUserByID 按 ID 查
func (db *DB) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, disabled, created_at, updated_at
		FROM users
		WHERE id = ?
	`, id)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return u, nil
}

// ListUsers 列所有用户（按 ID 升序）
func (db *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, username, password_hash, role, disabled, created_at, updated_at
		FROM users
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserDisabled 启用/禁用用户
func (db *DB) SetUserDisabled(ctx context.Context, id int64, disabled bool) error {
	val := 0
	if disabled {
		val = 1
	}
	_, err := db.ExecContext(ctx, `
		UPDATE users SET disabled = ?, updated_at = datetime('now') WHERE id = ?
	`, val, id)
	if err != nil {
		return fmt.Errorf("update user disabled: %w", err)
	}
	return nil
}

// DeleteUser 删除用户（CASCADE 会删 sessions，audit_log 保留 user_id=NULL）
func (db *DB) DeleteUser(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// CountAdmins 数 admin 用户数（用于防止删光所有 admin）
func (db *DB) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return n, nil
}

// scanner 抽象（既支持 *sql.Row 也支持 *sql.Rows）
type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*User, error) {
	var u User
	var disabled int
	var createdAt, updatedAt string
	if err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &disabled, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	u.Disabled = disabled != 0
	// SQLite datetime 是 UTC（'YYYY-MM-DD HH:MM:SS' 格式）
	parsedCreated, _ := time.Parse("2006-01-02 15:04:05", createdAt)
	parsedUpdated, _ := time.Parse("2006-01-02 15:04:05", updatedAt)
	if parsedCreated.IsZero() {
		parsedCreated = time.Now().UTC()
	}
	if parsedUpdated.IsZero() {
		parsedUpdated = time.Now().UTC()
	}
	u.CreatedAt = parsedCreated
	u.UpdatedAt = parsedUpdated
	return &u, nil
}

// isUniqueConstraintError 检查 SQLite UNIQUE 约束错误
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite error 包含 "UNIQUE constraint failed"
	return errors.Is(err, sql.ErrNoRows) == false &&
		(containsString(err.Error(), "UNIQUE constraint failed") ||
			containsString(err.Error(), "constraint failed"))
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
