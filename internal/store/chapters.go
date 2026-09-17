package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Chapter SQLite 表行
type Chapter struct {
	ID          int64     `json:"id"`
	ProjectID   int64     `json:"project_id"`
	N           int       `json:"n"`
	Title       string    `json:"title"`
	ContentPath string    `json:"content_path"`
	CharCount   int       `json:"char_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ChaptersStore chapters 表 CRUD
//
// 注: chapter content 仍在 filesystem (data/prose/第N章.md)，
// ContentPath 是相对路径（e.g. "data/prose/第001章.md"）。
// Sprint 15 双写策略：filesystem 为主存储，SQLite metadata index。
type ChaptersStore struct {
	db *DB
}

// NewChaptersStore 创建
func NewChaptersStore(db *DB) *ChaptersStore {
	return &ChaptersStore{db: db}
}

// List 列出项目的所有章节（按 n asc）
func (s *ChaptersStore) List(ctx context.Context, projectID int64) ([]*Chapter, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, n, title, content_path, char_count, created_at, updated_at
		FROM chapters
		WHERE project_id = ?
		ORDER BY n ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query chapters: %w", err)
	}
	defer rows.Close()
	return scanChapters(rows)
}

// Get 取单个
func (s *ChaptersStore) Get(ctx context.Context, id int64) (*Chapter, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, n, title, content_path, char_count, created_at, updated_at
		FROM chapters
		WHERE id = ?
	`, id)
	c, err := scanChapter(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query chapter %d: %w", id, err)
	}
	return c, nil
}

// GetByNumber 按 (project_id, n) 取
func (s *ChaptersStore) GetByNumber(ctx context.Context, projectID int64, n int) (*Chapter, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, n, title, content_path, char_count, created_at, updated_at
		FROM chapters
		WHERE project_id = ? AND n = ?
	`, projectID, n)
	c, err := scanChapter(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

// Upsert 插入或更新（按 project_id + n 唯一约束）
//
// 用于 chapters save 场景：filesystem 写完后更新 metadata
func (s *ChaptersStore) Upsert(ctx context.Context, projectID int64, n int, title, contentPath string, charCount int) (*Chapter, error) {
	if projectID <= 0 {
		return nil, errors.New("project_id required")
	}
	if n <= 0 {
		return nil, errors.New("n must be positive")
	}
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chapters (project_id, n, title, content_path, char_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, n) DO UPDATE SET
			title = excluded.title,
			content_path = excluded.content_path,
			char_count = excluded.char_count,
			updated_at = excluded.updated_at
	`, projectID, n, title, contentPath, charCount, now, now)
	if err != nil {
		return nil, fmt.Errorf("upsert chapter: %w", err)
	}
	return s.GetByNumber(ctx, projectID, n)
}

// Delete 删除
func (s *ChaptersStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM chapters WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete chapter: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByNumber 按 (project_id, n) 删除
func (s *ChaptersStore) DeleteByNumber(ctx context.Context, projectID int64, n int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM chapters WHERE project_id = ? AND n = ?`, projectID, n)
	if err != nil {
		return fmt.Errorf("delete chapter by number: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountByProject 统计项目章节数
func (s *ChaptersStore) CountByProject(ctx context.Context, projectID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chapters WHERE project_id = ?`, projectID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count chapters: %w", err)
	}
	return n, nil
}

func scanChapters(rows *sql.Rows) ([]*Chapter, error) {
	out := make([]*Chapter, 0)
	for rows.Next() {
		c, err := scanChapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanChapter(s rowScanner) (*Chapter, error) {
	var c Chapter
	var createdAt, updatedAt string
	err := s.Scan(&c.ID, &c.ProjectID, &c.N, &c.Title, &c.ContentPath, &c.CharCount, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	c.CreatedAt = parseTime(createdAt)
	c.UpdatedAt = parseTime(updatedAt)
	return &c, nil
}
