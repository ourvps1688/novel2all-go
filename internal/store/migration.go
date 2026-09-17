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

// schema003 Sprint 15：projects + chapters + chapter_reviews + llm_cache + adaptive_routing
//
// P1 阶段补齐 migration-mapping.md 要求的 5 张表：
//   - projects          (id, name, slug, description, genre, owner_id, created_at, updated_at)
//   - chapters          (id, project_id, n, title, content_path, char_count, created_at, updated_at)
//     注: content 仍在 filesystem (data/prose/), content_path 是相对路径
//   - chapter_reviews   (id, chapter_id, agent, verdict, issues_json, created_at)
//   - llm_cache         (key PRIMARY KEY, prompt_hash, response, model, hit_count, created_at, last_hit_at)
//   - adaptive_routing  (id, task, model, success_rate, total_calls, updated_at)
const schema003 = `
CREATE TABLE IF NOT EXISTS projects (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    genre       TEXT NOT NULL DEFAULT '',
    owner_id    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_projects_slug ON projects(slug);
CREATE INDEX IF NOT EXISTS idx_projects_owner ON projects(owner_id);
CREATE INDEX IF NOT EXISTS idx_projects_created_at ON projects(created_at);

CREATE TABLE IF NOT EXISTS chapters (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id    INTEGER NOT NULL,
    n             INTEGER NOT NULL,             -- chapter number (1-based)
    title         TEXT NOT NULL DEFAULT '',
    content_path  TEXT NOT NULL DEFAULT '',    -- relative to project_root, e.g. data/prose/第001章.md
    char_count    INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(project_id, n),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_chapters_project_id ON chapters(project_id);
CREATE INDEX IF NOT EXISTS idx_chapters_project_n ON chapters(project_id, n);

CREATE TABLE IF NOT EXISTS chapter_reviews (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    chapter_id   INTEGER NOT NULL,
    agent        TEXT NOT NULL DEFAULT '',    -- 'consistency' | 'deslop' | 'review' | custom
    verdict      TEXT NOT NULL DEFAULT '',    -- 'pass' | 'fail' | 'warn'
    issues_json  TEXT NOT NULL DEFAULT '{}',  -- JSON array of issues
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (chapter_id) REFERENCES chapters(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_reviews_chapter_id ON chapter_reviews(chapter_id);
CREATE INDEX IF NOT EXISTS idx_reviews_created_at ON chapter_reviews(created_at);

CREATE TABLE IF NOT EXISTS llm_cache (
    key         TEXT PRIMARY KEY,              -- SHA256(prompt + model + task) hex
    prompt_hash TEXT NOT NULL DEFAULT '',      -- 冗余存 SHA256(prompt) 便于 debug
    response    TEXT NOT NULL DEFAULT '',      -- JSON-encoded Response
    model       TEXT NOT NULL DEFAULT '',      -- e.g. 'MiniMax-M3' | 'deepseek-chat'
    hit_count   INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    last_hit_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_llm_cache_model ON llm_cache(model);
CREATE INDEX IF NOT EXISTS idx_llm_cache_last_hit ON llm_cache(last_hit_at);

CREATE TABLE IF NOT EXISTS adaptive_routing (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    task          TEXT NOT NULL,              -- 'WRITING' | 'CONSISTENCY' | 'EXTRACTION' | 'SUMMARIZATION' | 'COVER'
    model         TEXT NOT NULL,              -- e.g. 'MiniMax-M3'
    success_count INTEGER NOT NULL DEFAULT 0,
    total_count   INTEGER NOT NULL DEFAULT 0,
    success_rate  REAL NOT NULL DEFAULT 0,    -- success_count / total_count (cached)
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(task, model)
);

CREATE INDEX IF NOT EXISTS idx_routing_task ON adaptive_routing(task);
`

// Migrate 跑 schema migration（按版本顺序应用）
func (db *DB) Migrate(ctx context.Context) error {
	for _, stmt := range []string{schema001, schema002, schema003} {
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
