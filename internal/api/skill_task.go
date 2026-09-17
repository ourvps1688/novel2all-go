// Package api: skill_task.go — Sprint 28 SkillTaskManager.
//
// 对齐 Python V1 web/app.py: skill_tasks (skill execute task 注册表).
package api

import (
	"sync"
	"time"
)

// SkillTask 单个 skill execute 任务.
type SkillTask struct {
	ID          string
	Name        string
	ProjectRoot string
	Params      map[string]any
	StartedAt   time.Time
	FinishedAt  *time.Time
	Status      TaskStatus
	Output      string // 执行结果 (完成后填充)
	Err         error
}

// SkillTaskView 序列化到 JSON.
type SkillTaskView struct {
	ID          string         `json:"task_id"`
	Name        string         `json:"skill_name"`
	ProjectRoot string         `json:"project_root"`
	Params      map[string]any `json:"params"`
	StartedAt   time.Time      `json:"started_at"`
	FinishedAt  *time.Time     `json:"finished_at,omitempty"`
	Status      TaskStatus     `json:"status"`
	Output      string         `json:"output,omitempty"`
}

// SkillTaskManager skill 任务注册表.
type SkillTaskManager struct {
	mu    sync.RWMutex
	tasks map[string]*SkillTask
}

// NewSkillTaskManager 构造.
func NewSkillTaskManager() *SkillTaskManager {
	return &SkillTaskManager{tasks: make(map[string]*SkillTask)}
}

// Start 注册新任务.
func (m *SkillTaskManager) Start(name, projectRoot string, params map[string]any) *SkillTask {
	task := &SkillTask{
		ID:          generateTaskID(),
		Name:        name,
		ProjectRoot: projectRoot,
		Params:      params,
		StartedAt:   time.Now().UTC(),
		Status:      TaskStatusRunning,
	}
	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()
	return task
}

// Complete 标记完成.
func (m *SkillTaskManager) Complete(taskID, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return
	}
	now := time.Now().UTC()
	t.FinishedAt = &now
	t.Status = TaskStatusCompleted
	t.Output = output
}

// Fail 标记失败.
func (m *SkillTaskManager) Fail(taskID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return
	}
	now := time.Now().UTC()
	t.FinishedAt = &now
	t.Status = TaskStatusFailed
	t.Err = err
}

// Get 取任务.
func (m *SkillTaskManager) Get(taskID string) (*SkillTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[taskID]
	return t, ok
}

// List 列出所有任务.
func (m *SkillTaskManager) List() []SkillTaskView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SkillTaskView, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, skillTaskToView(t))
	}
	return out
}

// CleanupOlderThan 删除过期任务.
func (m *SkillTaskManager) CleanupOlderThan(maxAge time.Duration) {
	cutoff := time.Now().UTC().Add(-maxAge)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, t := range m.tasks {
		if t.FinishedAt != nil && t.FinishedAt.Before(cutoff) {
			delete(m.tasks, id)
		}
	}
}

func skillTaskToView(t *SkillTask) SkillTaskView {
	return SkillTaskView{
		ID:          t.ID,
		Name:        t.Name,
		ProjectRoot: t.ProjectRoot,
		Params:      t.Params,
		StartedAt:   t.StartedAt,
		FinishedAt:  t.FinishedAt,
		Status:      t.Status,
		Output:      t.Output,
	}
}
