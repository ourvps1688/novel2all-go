package api

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPipelineTaskManager_StartCreatesTask(t *testing.T) {
	m := NewPipelineTaskManager()
	defer m.CleanupOlderThan(0)

	task := m.Start(context.Background(), 5, "/tmp/proj", "story-long-write")
	if task.ID == "" {
		t.Error("task ID should not be empty")
	}
	if task.Chapter != 5 {
		t.Errorf("Chapter = %d, want 5", task.Chapter)
	}
	if task.Status != TaskStatusRunning {
		t.Errorf("Status = %s, want running", task.Status)
	}
}

func TestPipelineTaskManager_Get(t *testing.T) {
	m := NewPipelineTaskManager()
	task := m.Start(context.Background(), 1, "/p", "skill1")

	got, ok := m.Get(task.ID)
	if !ok {
		t.Fatal("Get should return true for existing task")
	}
	if got.ID != task.ID {
		t.Errorf("ID mismatch")
	}

	_, ok = m.Get("nonexistent")
	if ok {
		t.Error("Get should return false for missing task")
	}
}

func TestPipelineTaskManager_Cancel(t *testing.T) {
	m := NewPipelineTaskManager()
	task := m.Start(context.Background(), 1, "/p", "skill1")

	err := m.Cancel(task.ID)
	if err != nil {
		t.Errorf("Cancel error: %v", err)
	}

	got, _ := m.Get(task.ID)
	if got.Status != TaskStatusCancelled {
		t.Errorf("Status = %s, want cancelled", got.Status)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt should be set after Cancel")
	}
}

func TestPipelineTaskManager_CancelNotFound(t *testing.T) {
	m := NewPipelineTaskManager()
	err := m.Cancel("nonexistent")
	if !errors.Is(err, ErrPipelineTaskNotFound) {
		t.Errorf("expected ErrPipelineTaskNotFound, got %v", err)
	}
}

func TestPipelineTaskManager_Complete(t *testing.T) {
	m := NewPipelineTaskManager()
	task := m.Start(context.Background(), 1, "/p", "skill1")

	m.Complete(task.ID, "/output/chapter.md", 2500)

	got, _ := m.Get(task.ID)
	if got.Status != TaskStatusCompleted {
		t.Errorf("Status = %s, want completed", got.Status)
	}
	if got.OutputPath != "/output/chapter.md" {
		t.Errorf("OutputPath = %s", got.OutputPath)
	}
	if got.CharCount != 2500 {
		t.Errorf("CharCount = %d, want 2500", got.CharCount)
	}
}

func TestPipelineTaskManager_Fail(t *testing.T) {
	m := NewPipelineTaskManager()
	task := m.Start(context.Background(), 1, "/p", "skill1")

	m.Fail(task.ID, errors.New("llm timeout"))

	got, _ := m.Get(task.ID)
	if got.Status != TaskStatusFailed {
		t.Errorf("Status = %s, want failed", got.Status)
	}
	if got.Err == nil {
		t.Error("Err should be set")
	}
}

func TestPipelineTaskManager_ListActive(t *testing.T) {
	m := NewPipelineTaskManager()
	t1 := m.Start(context.Background(), 1, "/p", "s1")
	t2 := m.Start(context.Background(), 2, "/p", "s2")
	_ = m.Start(context.Background(), 3, "/p", "s3")

	m.Complete(t2.ID, "/o", 100)

	active := m.ListActive()
	if len(active) != 2 {
		t.Errorf("ListActive count = %d, want 2", len(active))
	}
	// 验证 t1 还在, t2 已完成不计入
	found := false
	for _, v := range active {
		if v.ID == t1.ID {
			found = true
		}
	}
	if !found {
		t.Error("t1 should be in active list")
	}
}

func TestPipelineTaskManager_Cleanup(t *testing.T) {
	m := NewPipelineTaskManager()
	task := m.Start(context.Background(), 1, "/p", "s")
	m.Complete(task.ID, "/o", 100)

	// 直接改 FinishedAt 模拟 1 小时前
	m.mu.Lock()
	m.tasks[task.ID].FinishedAt = ptrTime(time.Now().Add(-1 * time.Hour))
	m.mu.Unlock()

	m.CleanupOlderThan(30 * time.Minute)
	if _, ok := m.Get(task.ID); ok {
		t.Error("old task should be cleaned up")
	}
}

func TestPipelineTaskManager_ConcurrentStartCancel(t *testing.T) {
	m := NewPipelineTaskManager()
	var wg sync.WaitGroup

	// 并发启 10 个任务
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Start(context.Background(), i, "/p", "s")
		}(i)
	}
	wg.Wait()

	// 并发取消所有
	tasks := m.List()
	for _, v := range tasks {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = m.Cancel(id)
		}(v.ID)
	}
	wg.Wait()

	active := m.ListActive()
	if len(active) != 0 {
		t.Errorf("expected 0 active after cancel all, got %d", len(active))
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
