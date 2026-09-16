package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// WriteTaskStatus 写作任务状态
type WriteTaskStatus string

const (
	WriteTaskPending   WriteTaskStatus = "pending"
	WriteTaskRunning   WriteTaskStatus = "running"
	WriteTaskCompleted WriteTaskStatus = "completed"
	WriteTaskFailed    WriteTaskStatus = "failed"
	WriteTaskCancelled WriteTaskStatus = "cancelled"
)

// WriteTask 表示一次流式写作任务
//
// P1-F 切片 3: 内存存储 + goroutine 执行；P2 阶段会持久化到 SQLite。
type WriteTask struct {
	ID           string          `json:"task_id"`
	Status       WriteTaskStatus `json:"status"`
	Chapter      int             `json:"chapter"`
	Skill        string          `json:"skill"`
	ProjectRoot  string          `json:"project_root"`
	MinChars     int             `json:"min_chars"`
	SkipPreWrite bool            `json:"skip_pre_write"`

	// 进度
	StartedAt    time.Time  `json:"started_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	CharsWritten int        `json:"chars_written"`
	Content      string     `json:"content,omitempty"`

	// 取消信号（cancel() 后 ctx.Done() 触发）
	cancel context.CancelFunc
}

// TaskManager 进程内任务管理器
type TaskManager struct {
	mu    sync.RWMutex
	tasks map[string]*WriteTask
}

// NewTaskManager 创建
func NewTaskManager() *TaskManager {
	return &TaskManager{tasks: make(map[string]*WriteTask)}
}

// Create 创建任务并返回
func (m *TaskManager) Create(chapter int, skill, projectRoot string, minChars int, skipPreWrite bool, cancel context.CancelFunc) *WriteTask {
	id := newTaskID()
	now := time.Now()
	t := &WriteTask{
		ID:           id,
		Status:       WriteTaskPending,
		Chapter:      chapter,
		Skill:        skill,
		ProjectRoot:  projectRoot,
		MinChars:     minChars,
		SkipPreWrite: skipPreWrite,
		StartedAt:    now,
		UpdatedAt:    now,
		cancel:       cancel,
	}
	m.mu.Lock()
	m.tasks[id] = t
	m.mu.Unlock()
	return t
}

// Get 取任务
func (m *TaskManager) Get(id string) (*WriteTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	return t, ok
}

// List 列出所有任务（按 StartedAt 倒序）
func (m *TaskManager) List() []*WriteTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*WriteTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t)
	}
	return out
}

// ListActive 列出活跃任务（pending/running）
func (m *TaskManager) ListActive() []*WriteTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*WriteTask, 0)
	for _, t := range m.tasks {
		if t.Status == WriteTaskPending || t.Status == WriteTaskRunning {
			out = append(out, t)
		}
	}
	return out
}

// Cancel 取消任务（如果存在且未完成）
func (m *TaskManager) Cancel(id string) (*WriteTask, error) {
	m.mu.Lock()
	t, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return nil, ErrTaskNotFound
	}
	if t.Status == WriteTaskCompleted || t.Status == WriteTaskFailed || t.Status == WriteTaskCancelled {
		m.mu.Unlock()
		return nil, ErrTaskDone
	}
	if t.cancel != nil {
		t.cancel()
	}
	t.Status = WriteTaskCancelled
	t.UpdatedAt = time.Now()
	now := time.Now()
	t.CompletedAt = &now
	m.mu.Unlock()
	return t, nil
}

// Remove 删除任务
func (m *TaskManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
}

// UpdateProgress 更新进度（线程安全）
func (t *WriteTask) UpdateProgress(chars int, content string) {
	t.CharsWritten = chars
	t.Content = content
	t.UpdatedAt = time.Now()
}

// MarkRunning 标记任务为 running
func (t *WriteTask) MarkRunning() {
	t.Status = WriteTaskRunning
	t.UpdatedAt = time.Now()
}

// MarkCompleted 标记任务完成
func (t *WriteTask) MarkCompleted() {
	t.Status = WriteTaskCompleted
	t.UpdatedAt = time.Now()
	now := time.Now()
	t.CompletedAt = &now
}

// MarkFailed 标记任务失败
func (t *WriteTask) MarkFailed() {
	t.Status = WriteTaskFailed
	t.UpdatedAt = time.Now()
	now := time.Now()
	t.CompletedAt = &now
}

// MarkCancelled 标记任务取消
func (t *WriteTask) MarkCancelled() {
	t.Status = WriteTaskCancelled
	t.UpdatedAt = time.Now()
	now := time.Now()
	t.CompletedAt = &now
}

// Errors
var (
	ErrTaskNotFound = errors.New("task not found")
	ErrTaskDone     = errors.New("task already done")
)

// newTaskID 生成 16 字符随机 task ID
func newTaskID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
