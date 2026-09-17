// Package api: pipeline_task.go — Sprint 28 PipelineTaskManager.
//
// 对齐 Python V1 web/app.py: active_pipelines (asyncio.Task 注册表).
//
// 用途: 跟踪当前正在运行的写作 / skill execute 任务, 支持:
//   - /api/write/active     列出活跃任务
//   - /api/write/cancel/{id} 取消任务
//   - SSE 流式 (chunk 推送)
package api

import (
	"context"
	"errors"
	"sync"
	"time"
)

// TaskStatus 任务状态.
type TaskStatus string

const (
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusCancelled TaskStatus = "cancelled"
	TaskStatusFailed    TaskStatus = "failed"
)

// PipelineTask 单个 pipeline 任务 (写作 / skill execute).
type PipelineTask struct {
	ID          string
	Chapter     int
	ProjectRoot string
	Skill       string
	StartedAt   time.Time
	FinishedAt  *time.Time
	Status      TaskStatus

	// SSE 流式支持
	ChunkCh chan string // SSE chunk 推送 (writer)
	Done    chan struct{}
	Err     error
	Cancel  context.CancelFunc

	// 输出路径 (完成后填充)
	OutputPath string
	CharCount  int
}

// PipelineTaskView 序列化到 JSON (对外暴露).
type PipelineTaskView struct {
	ID          string     `json:"task_id"`
	Chapter     int        `json:"chapter"`
	ProjectRoot string     `json:"project_root"`
	Skill       string     `json:"skill"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Status      TaskStatus `json:"status"`
	OutputPath  string     `json:"output_path,omitempty"`
	CharCount   int        `json:"char_count,omitempty"`
}

// PipelineTaskManager 全局任务注册表.
type PipelineTaskManager struct {
	mu    sync.RWMutex
	tasks map[string]*PipelineTask
}

// NewPipelineTaskManager 构造.
func NewPipelineTaskManager() *PipelineTaskManager {
	return &PipelineTaskManager{tasks: make(map[string]*PipelineTask)}
}

// Start 启动新任务 (返回 task, caller 持有 cancel fn).
//
// chunkCh capacity=16 (防止写方阻塞; SSE slow consumer 场景).
func (m *PipelineTaskManager) Start(ctx context.Context, chapter int, projectRoot, skill string) *PipelineTask {
	taskCtx, cancel := context.WithCancel(ctx)
	task := &PipelineTask{
		ID:          generateTaskID(),
		Chapter:     chapter,
		ProjectRoot: projectRoot,
		Skill:       skill,
		StartedAt:   time.Now().UTC(),
		Status:      TaskStatusRunning,
		ChunkCh:     make(chan string, 16),
		Done:        make(chan struct{}),
		Cancel:      cancel,
	}
	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()

	go func() {
		<-taskCtx.Done()
		// 不主动 mark status, 由 caller (writing pipeline) 标记
	}()

	return task
}

// Cancel 取消任务 (标记 cancelled + 调 task.Cancel + 关闭 ChunkCh/Done).
//
// 返回 ErrPipelineTaskNotFound 如果 task 不存在.
//
// 并发安全: 全程持锁, 避免 task.Status 与 Complete/Fail 竞态.
func (m *PipelineTaskManager) Cancel(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[taskID]
	if !ok {
		return ErrPipelineTaskNotFound
	}
	// 已完成的任务不能被 cancel 覆盖
	if task.Status != TaskStatusRunning {
		return nil
	}
	task.Cancel() // context.CancelFunc, 内部并发安全
	task.Status = TaskStatusCancelled
	now := time.Now().UTC()
	task.FinishedAt = &now
	// 关闭 channel (通知 SSE handler 退出)
	closeIfOpen(task.Done)
	return nil
}

// Complete 标记任务完成.
func (m *PipelineTaskManager) Complete(taskID, outputPath string, charCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[taskID]
	if !ok {
		return
	}
	now := time.Now().UTC()
	task.FinishedAt = &now
	task.Status = TaskStatusCompleted
	task.OutputPath = outputPath
	task.CharCount = charCount
	closeIfOpen(task.Done)
}

// Fail 标记任务失败.
func (m *PipelineTaskManager) Fail(taskID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[taskID]
	if !ok {
		return
	}
	now := time.Now().UTC()
	task.FinishedAt = &now
	task.Status = TaskStatusFailed
	task.Err = err
	closeIfOpen(task.Done)
}

// List 列出所有任务 (按 StartedAt 倒序).
func (m *PipelineTaskManager) List() []PipelineTaskView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PipelineTaskView, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, taskToView(t))
	}
	// 按 StartedAt 倒序
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].StartedAt.After(out[i].StartedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// ListActive 列出正在运行的任务.
func (m *PipelineTaskManager) ListActive() []PipelineTaskView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PipelineTaskView, 0)
	for _, t := range m.tasks {
		if t.Status == TaskStatusRunning {
			out = append(out, taskToView(t))
		}
	}
	return out
}

// Get 取单任务.
func (m *PipelineTaskManager) Get(taskID string) (*PipelineTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[taskID]
	return t, ok
}

// CleanupOlderThan 删除 N 分钟前完成的任务 (避免内存泄漏).
//
// 默认保留 30 分钟 (Python V0.31 active_pipelines 同样).
func (m *PipelineTaskManager) CleanupOlderThan(maxAge time.Duration) {
	cutoff := time.Now().UTC().Add(-maxAge)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, t := range m.tasks {
		if t.FinishedAt != nil && t.FinishedAt.Before(cutoff) {
			delete(m.tasks, id)
		}
	}
}

func taskToView(t *PipelineTask) PipelineTaskView {
	return PipelineTaskView{
		ID:          t.ID,
		Chapter:     t.Chapter,
		ProjectRoot: t.ProjectRoot,
		Skill:       t.Skill,
		StartedAt:   t.StartedAt,
		FinishedAt:  t.FinishedAt,
		Status:      t.Status,
		OutputPath:  t.OutputPath,
		CharCount:   t.CharCount,
	}
}

// generateTaskID uuid4-like 8-char hex.
func generateTaskID() string {
	// V0 简化: 用 timestamp + counter, 不引 uuid dep
	return time.Now().UTC().Format("20060102150405") + "-" + randomHex(8)
}

// randomHex 返回 n 字符 hex.
var (
	hexCounter uint32
	hexMu      sync.Mutex
)

func randomHex(n int) string {
	hexMu.Lock()
	hexCounter++
	c := hexCounter
	hexMu.Unlock()
	// 用 ns timestamp + counter 生成伪 unique
	ns := time.Now().UTC().UnixNano()
	v := uint64(ns) ^ uint64(c)
	out := make([]byte, n)
	const hexChars = "0123456789abcdef"
	for i := 0; i < n; i++ {
		out[i] = hexChars[v&0xF]
		v >>= 4
	}
	return string(out)
}

// closeIfOpen 安全关闭 channel (防止 double close panic).
func closeIfOpen(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}

// ErrPipelineTaskNotFound 任务不存在错误.
var ErrPipelineTaskNotFound = errors.New("pipeline task not found")
