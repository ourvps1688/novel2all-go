package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ChapterReview chapter_reviews 表行
type ChapterReview struct {
	ID         int64     `json:"id"`
	ChapterID  int64     `json:"chapter_id"`
	Agent      string    `json:"agent"`       // 'consistency' | 'deslop' | 'review' | custom
	Verdict    string    `json:"verdict"`     // 'pass' | 'fail' | 'warn'
	IssuesJSON string    `json:"issues_json"` // JSON array of issues
	CreatedAt  time.Time `json:"created_at"`
}

// ReviewsStore chapter_reviews 表 CRUD
type ReviewsStore struct {
	db *DB
}

// NewReviewsStore 创建
func NewReviewsStore(db *DB) *ReviewsStore {
	return &ReviewsStore{db: db}
}

// Create 插入一条 review
func (s *ReviewsStore) Create(ctx context.Context, chapterID int64, agent, verdict, issuesJSON string) (*ChapterReview, error) {
	if chapterID <= 0 {
		return nil, errors.New("chapter_id required")
	}
	if agent == "" {
		return nil, errors.New("agent required")
	}
	if verdict == "" {
		return nil, errors.New("verdict required")
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO chapter_reviews (chapter_id, agent, verdict, issues_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, chapterID, agent, verdict, issuesJSON, now)
	if err != nil {
		return nil, fmt.Errorf("insert review: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

// Get 取单条
func (s *ReviewsStore) Get(ctx context.Context, id int64) (*ChapterReview, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, chapter_id, agent, verdict, issues_json, created_at
		FROM chapter_reviews
		WHERE id = ?
	`, id)
	r, err := scanReview(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query review %d: %w", id, err)
	}
	return r, nil
}

// ListByChapter 列某章节所有 review（按 created_at desc）
func (s *ReviewsStore) ListByChapter(ctx context.Context, chapterID int64) ([]*ChapterReview, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, chapter_id, agent, verdict, issues_json, created_at
		FROM chapter_reviews
		WHERE chapter_id = ?
		ORDER BY created_at DESC
	`, chapterID)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanReviews(rows)
}

// ListByAgent 按 agent 列 review（用于聚合统计）
func (s *ReviewsStore) ListByAgent(ctx context.Context, agent string, limit int) ([]*ChapterReview, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, chapter_id, agent, verdict, issues_json, created_at
		FROM chapter_reviews
		WHERE agent = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, agent, limit)
	if err != nil {
		return nil, fmt.Errorf("list reviews by agent: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanReviews(rows)
}

func scanReviews(rows *sql.Rows) ([]*ChapterReview, error) {
	out := make([]*ChapterReview, 0)
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanReview(s rowScanner) (*ChapterReview, error) {
	var r ChapterReview
	var createdAt string
	err := s.Scan(&r.ID, &r.ChapterID, &r.Agent, &r.Verdict, &r.IssuesJSON, &createdAt)
	if err != nil {
		return nil, err
	}
	r.CreatedAt = parseTime(createdAt)
	return &r, nil
}
