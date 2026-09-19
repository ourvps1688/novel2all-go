package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RoutingEntry adaptive_routing 表行
type RoutingEntry struct {
	Task         string    `json:"task"`
	Model        string    `json:"model"`
	SuccessCount int64     `json:"success_count"`
	TotalCount   int64     `json:"total_count"`
	SuccessRate  float64   `json:"success_rate"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// RoutingStore adaptive_routing 表 CRUD
type RoutingStore struct {
	db *DB
}

// NewRoutingStore 创建
func NewRoutingStore(db *DB) *RoutingStore {
	return &RoutingStore{db: db}
}

// RecordResult 记录一次 LLM 调用结果，自动更新 success_rate
//
// 该方法做原子 upsert：
//   - 第一次：(task, model) 不存在 → 插入 success/total 1/1
//   - 后续：total_count++, success_count += success ? 1 : 0
//   - success_rate 自动重算
func (s *RoutingStore) RecordResult(ctx context.Context, task, model string, success bool) error {
	if task == "" || model == "" {
		return errors.New("task and model required")
	}
	now := time.Now().UTC()
	successDelta := 0
	if success {
		successDelta = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO adaptive_routing (task, model, success_count, total_count, success_rate, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(task, model) DO UPDATE SET
			success_count = success_count + ?,
			total_count = total_count + 1,
			success_rate = CAST(success_count + ? AS REAL) / (total_count + 1),
			updated_at = ?
	`, task, model, successDelta, float64(successDelta), now, successDelta, successDelta, now)
	if err != nil {
		return fmt.Errorf("upsert routing: %w", err)
	}
	return nil
}

// Get 取 (task, model) 路由条目
func (s *RoutingStore) Get(ctx context.Context, task, model string) (*RoutingEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT task, model, success_count, total_count, success_rate, updated_at
		FROM adaptive_routing
		WHERE task = ? AND model = ?
	`, task, model)
	r, err := scanRouting(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query routing: %w", err)
	}
	return r, nil
}

// ListByTask 按 task 列路由条目
func (s *RoutingStore) ListByTask(ctx context.Context, task string) ([]*RoutingEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task, model, success_count, total_count, success_rate, updated_at
		FROM adaptive_routing
		WHERE task = ?
		ORDER BY success_rate DESC
	`, task)
	if err != nil {
		return nil, fmt.Errorf("list routing by task: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRoutings(rows)
}

// BestModelForTask 找 task 表现最好的 model（success_rate 最高 + total_count >= minSamples）
//
// 用于 P2 阶段自动 model routing
func (s *RoutingStore) BestModelForTask(ctx context.Context, task string, minSamples int64) (*RoutingEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT task, model, success_count, total_count, success_rate, updated_at
		FROM adaptive_routing
		WHERE task = ? AND total_count >= ?
		ORDER BY success_rate DESC, total_count DESC
		LIMIT 1
	`, task, minSamples)
	r, err := scanRouting(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

func scanRoutings(rows *sql.Rows) ([]*RoutingEntry, error) {
	out := make([]*RoutingEntry, 0)
	for rows.Next() {
		r, err := scanRouting(rows)
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

func scanRouting(s rowScanner) (*RoutingEntry, error) {
	var r RoutingEntry
	var updatedAt string
	err := s.Scan(&r.Task, &r.Model, &r.SuccessCount, &r.TotalCount, &r.SuccessRate, &updatedAt)
	if err != nil {
		return nil, err
	}
	r.UpdatedAt = parseTime(updatedAt)
	return &r, nil
}
