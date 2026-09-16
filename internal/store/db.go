// Package store 提供 SQLite 存储 + 表 CRUD。
//
// P1-E 阶段实现 3 张核心表：
//   - users       (账号 + bcrypt 哈希 + role + disabled)
//   - sessions    (token + user_id + expires_at + ip)
//   - audit_log   (event_type + user_id + ip + success + detail)
//
// 使用 modernc.org/sqlite（纯 Go，无需 CGO）。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 注册 sqlite driver
)

// DB 是 SQLite 数据库句柄
type DB struct {
	*sql.DB
}

// Open 打开 SQLite 数据库，自动启用 WAL + foreign keys
//
// 自动创建父目录（如果 DSN 是文件路径）
func Open(ctx context.Context, dsn string) (*DB, error) {
	// 如果 DSN 是文件路径（如 "data/foo.db"），自动 mkdir 父目录
	if dir := filepath.Dir(dsn); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite 限制：串行写，并发读
	// 调高连接数到 1（写）+ 5（读）以避免 lock contention
	db.SetMaxOpenConns(1) // modernc.org/sqlite 单连接最稳
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	// 验证连通
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	// 启用外键 + WAL
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	return &DB{DB: db}, nil
}

// Close 关闭数据库
func (db *DB) Close() error {
	return db.DB.Close()
}
