package store

import (
	"context"
	"fmt"
)

// schema001 是 P1-E 初始 schema（users / sessions / audit_log）
const schema001 = `
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user',  -- 'admin' | 'user'
    disabled      INTEGER NOT NULL DEFAULT 0,    -- 0 = active, 1 = disabled
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

CREATE TABLE IF NOT EXISTS sessions (
    token       TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL,
    expires_at  TEXT NOT NULL,
    ip          TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type  TEXT NOT NULL,    -- 'login' | 'login_fail' | 'logout' | 'create_user' | 'delete_user' | etc.
    user_id     INTEGER,         -- NULL if event relates to non-existent user (e.g. login_fail with bad username)
    username    TEXT NOT NULL DEFAULT '',
    ip          TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    success     INTEGER NOT NULL DEFAULT 1,  -- 0 = fail, 1 = success
    detail      TEXT NOT NULL DEFAULT '',   -- JSON or free-form
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_user_id ON audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_event_type ON audit_log(event_type);
CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_log(created_at);
`

// schema002 P1-F 切片 8：project_memberships（项目分享 + users/projects 关联）
const schema002 = `
CREATE TABLE IF NOT EXISTS project_memberships (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    project_path  TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer',  -- 'owner' | 'editor' | 'viewer'
    granted_by    INTEGER NOT NULL DEFAULT 0,
    granted_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, project_path),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_pm_project_path ON project_memberships(project_path);
CREATE INDEX IF NOT EXISTS idx_pm_user_id ON project_memberships(user_id);
`

// Migrate 跑 schema migration（按版本顺序应用）
func (db *DB) Migrate(ctx context.Context) error {
	for _, stmt := range []string{schema001, schema002} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply migration: %w", err)
		}
	}
	return nil
}

// EnsureAdminUser 确保至少有一个 admin 用户（从环境变量读 ADMIN_USER/ADMIN_PASS）
// 如果用户已存在则不动；如果不存在则创建
func (db *DB) EnsureAdminUser(ctx context.Context, username, passwordHash string) error {
	if username == "" || passwordHash == "" {
		return fmt.Errorf("admin username and password hash required")
	}
	_, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO users (username, password_hash, role)
		VALUES (?, ?, 'admin')
	`, username, passwordHash)
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}
	return nil
}
