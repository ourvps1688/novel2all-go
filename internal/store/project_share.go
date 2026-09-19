package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ProjectMembership 项目成员关系
//
// user_id 和 project_path 唯一 (UNIQUE 约束)
// role: 'owner' | 'editor' | 'viewer'
type ProjectMembership struct {
	ID          int64
	UserID      int64
	ProjectPath string
	Role        string
	GrantedBy   int64
	GrantedAt   string
}

// ErrMembershipNotFound 成员关系不存在
var ErrMembershipNotFound = errors.New("project membership not found")

// ErrMembershipExists 成员关系已存在
var ErrMembershipExists = errors.New("project membership already exists")

// GrantProjectAccess 给用户授权项目访问
//
// 返回 ErrMembershipExists 如果已存在（可调用方决定 upsert）
func (db *DB) GrantProjectAccess(ctx context.Context, userID int64, projectPath, role string, grantedBy int64) (*ProjectMembership, error) {
	res, err := db.ExecContext(ctx, `
		INSERT INTO project_memberships (user_id, project_path, role, granted_by)
		VALUES (?, ?, ?, ?)
	`, userID, projectPath, role, grantedBy)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrMembershipExists
		}
		return nil, fmt.Errorf("grant project access: %w", err)
	}
	_, err = res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return db.GetProjectMembership(ctx, userID, projectPath)
}

// GetProjectMembership 取单个成员关系
func (db *DB) GetProjectMembership(ctx context.Context, userID int64, projectPath string) (*ProjectMembership, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, user_id, project_path, role, granted_by, granted_at
		FROM project_memberships
		WHERE user_id = ? AND project_path = ?
	`, userID, projectPath)
	m, err := scanMembership(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMembershipNotFound
		}
		return nil, err
	}
	return m, nil
}

// GetProjectRole 取用户对某项目的角色（不存在返回空字符串 + nil error）
func (db *DB) GetProjectRole(ctx context.Context, userID int64, projectPath string) (string, error) {
	var role string
	err := db.QueryRowContext(ctx, `
		SELECT role FROM project_memberships
		WHERE user_id = ? AND project_path = ?
	`, userID, projectPath).Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return role, nil
}

// RevokeProjectAccess 撤销用户项目访问
func (db *DB) RevokeProjectAccess(ctx context.Context, userID int64, projectPath string) error {
	res, err := db.ExecContext(ctx, `
		DELETE FROM project_memberships WHERE user_id = ? AND project_path = ?
	`, userID, projectPath)
	if err != nil {
		return fmt.Errorf("revoke project access: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrMembershipNotFound
	}
	return nil
}

// ListProjectMembers 列出项目的所有成员
func (db *DB) ListProjectMembers(ctx context.Context, projectPath string) ([]*ProjectMembership, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, project_path, role, granted_by, granted_at
		FROM project_memberships
		WHERE project_path = ?
		ORDER BY granted_at DESC
	`, projectPath)
	if err != nil {
		return nil, fmt.Errorf("list project members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*ProjectMembership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListUserProjects 列出用户能访问的项目
func (db *DB) ListUserProjects(ctx context.Context, userID int64) ([]*ProjectMembership, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, project_path, role, granted_by, granted_at
		FROM project_memberships
		WHERE user_id = ?
		ORDER BY granted_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user projects: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*ProjectMembership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateProjectRole 更新角色（owner ↔ editor ↔ viewer）
func (db *DB) UpdateProjectRole(ctx context.Context, userID int64, projectPath, role string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE project_memberships SET role = ?
		WHERE user_id = ? AND project_path = ?
	`, role, userID, projectPath)
	if err != nil {
		return fmt.Errorf("update project role: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrMembershipNotFound
	}
	return nil
}

func scanMembership(s scanner) (*ProjectMembership, error) {
	var m ProjectMembership
	if err := s.Scan(&m.ID, &m.UserID, &m.ProjectPath, &m.Role, &m.GrantedBy, &m.GrantedAt); err != nil {
		return nil, err
	}
	return &m, nil
}
