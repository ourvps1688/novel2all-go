package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Project SQLite 表行
//
// 与 api.Project 字段对齐（json tag 一致），便于转换
type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Genre       string    `json:"genre,omitempty"`
	OwnerID     int64     `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectsStore projects 表 CRUD
type ProjectsStore struct {
	db *DB
}

// NewProjectsStore 创建
func NewProjectsStore(db *DB) *ProjectsStore {
	return &ProjectsStore{db: db}
}

// List 列出所有项目（按 created_at desc）
func (s *ProjectsStore) List(ctx context.Context) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, slug, description, genre, owner_id, created_at, updated_at
		FROM projects
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanProjects(rows)
}

// Get 取单个
func (s *ProjectsStore) Get(ctx context.Context, id int64) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, slug, description, genre, owner_id, created_at, updated_at
		FROM projects
		WHERE id = ?
	`, id)
	p, err := scanProject(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query project %d: %w", id, err)
	}
	return p, nil
}

// GetBySlug 按 slug 取
func (s *ProjectsStore) GetBySlug(ctx context.Context, slug string) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, slug, description, genre, owner_id, created_at, updated_at
		FROM projects
		WHERE slug = ?
	`, slug)
	p, err := scanProject(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query project slug %q: %w", slug, err)
	}
	return p, nil
}

// Create 创建项目（自动分配 ID）
func (s *ProjectsStore) Create(ctx context.Context, name, slug, desc, genre string, ownerID int64) (*Project, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if slug == "" {
		return nil, errors.New("slug is required")
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO projects (name, slug, description, genre, owner_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, name, slug, desc, genre, ownerID, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

// Update 更新（name/desc/genre 都可选）
func (s *ProjectsStore) Update(ctx context.Context, id int64, name, desc, genre string) (*Project, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE projects SET
			name = COALESCE(NULLIF(?, ''), name),
			description = COALESCE(NULLIF(?, ''), description),
			genre = COALESCE(NULLIF(?, ''), genre),
			updated_at = ?
		WHERE id = ?
	`, name, desc, genre, now, id)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

// Delete 删除（CASCADE chapters/reviews）
func (s *ProjectsStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByOwner 按 owner_id 列项目
func (s *ProjectsStore) ListByOwner(ctx context.Context, ownerID int64) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, slug, description, genre, owner_id, created_at, updated_at
		FROM projects
		WHERE owner_id = ?
		ORDER BY created_at DESC
	`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("query projects by owner: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanProjects(rows)
}

// scanProjects 扫描多行
func scanProjects(rows *sql.Rows) ([]*Project, error) {
	out := make([]*Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// scanProject 扫描单行（rows 或 row 都支持）
func scanProject(s rowScanner) (*Project, error) {
	var p Project
	var createdAt, updatedAt string
	err := s.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Genre, &p.OwnerID, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	return &p, nil
}

// scanner 抽象 Scan 方法（*sql.Row + *sql.Rows 都支持）
type rowScanner interface {
	Scan(dest ...any) error
}

// parseTime SQLite datetime 转 Go time.Time
func parseTime(s string) time.Time {
	// SQLite 格式: "2026-09-17 07:40:38" (UTC, no timezone)
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// ErrNotFound not found
var ErrNotFound = errors.New("not found")

// DeleteAll 清空 projects 表（state persistence 用）
//
// CASCADE 自动删除 chapters + chapter_reviews。
func (s *ProjectsStore) DeleteAll(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM projects"); err != nil {
		return fmt.Errorf("delete all projects: %w", err)
	}
	return nil
}
