package api

import (
	"context"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// ProjectsRepo ProjectsStore + state persistence 共用的 repo 接口
//
// 兼容现有的 ProjectStore (内存) + store.ProjectsStore (SQLite)，便于 Sprint 15 commit D 切换
type ProjectsRepo interface {
	List() []*Project
	Get(id int64) (*Project, error)
	Create(name, slug, desc string, ownerID int64, genre string) (*Project, error)
	Update(id int64, name, desc, genre string) (*Project, error)
	Delete(id int64) error
	RestoreAll(projects []*Project) error // state persistence 用
}

// SQLiteProjectsAdapter 把 store.ProjectsStore 包成 ProjectsRepo 接口
//
// 解决 api.Project (json tag) 与 store.Project (同字段) 之间的转换。
type SQLiteProjectsAdapter struct {
	s *store.ProjectsStore
}

// NewSQLiteProjectsAdapter 包装 store.ProjectsStore
func NewSQLiteProjectsAdapter(s *store.ProjectsStore) *SQLiteProjectsAdapter {
	return &SQLiteProjectsAdapter{s: s}
}

// List 列出所有项目（API 兼容）
func (a *SQLiteProjectsAdapter) List() []*Project {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := a.s.List(ctx)
	if err != nil {
		return nil
	}
	out := make([]*Project, 0, len(rows))
	for _, p := range rows {
		out = append(out, projectStoreToAPI(p))
	}
	return out
}

// Get 取单个
func (a *SQLiteProjectsAdapter) Get(id int64) (*Project, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := a.s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return projectStoreToAPI(p), nil
}

// Create 插入（参数对应 store 层）
func (a *SQLiteProjectsAdapter) Create(name, slug, desc string, ownerID int64, genre string) (*Project, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := a.s.Create(ctx, name, slug, desc, genre, ownerID)
	if err != nil {
		return nil, err
	}
	return projectStoreToAPI(p), nil
}

// Update 更新
func (a *SQLiteProjectsAdapter) Update(id int64, name, desc, genre string) (*Project, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := a.s.Update(ctx, id, name, desc, genre)
	if err != nil {
		return nil, err
	}
	return projectStoreToAPI(p), nil
}

// Delete 删除
func (a *SQLiteProjectsAdapter) Delete(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.s.Delete(ctx, id)
}

// RestoreAll state persistence 用（清空表 + 重新插入）
//
// 实现 ProjectsRepo interface 要求；用于 /api/state/reload 恢复 projects 表
func (a *SQLiteProjectsAdapter) RestoreAll(projects []*Project) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 清空表
	if err := a.s.DeleteAll(ctx); err != nil {
		return err
	}

	// 2. 重新插入
	for _, p := range projects {
		_, err := a.s.Create(ctx, p.Name, p.Slug, p.Description, p.Genre, p.OwnerID)
		if err != nil {
			return err
		}
	}
	return nil
}

// projectStoreToAPI store.Project → api.Project
func projectStoreToAPI(p *store.Project) *Project {
	if p == nil {
		return nil
	}
	return &Project{
		ID:          p.ID,
		Name:        p.Name,
		Slug:        p.Slug,
		Description: p.Description,
		Genre:       p.Genre,
		OwnerID:     p.OwnerID,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// Compile-time 接口实现检查
var _ ProjectsRepo = (*SQLiteProjectsAdapter)(nil)
var _ ProjectsRepo = (*ProjectStore)(nil)
