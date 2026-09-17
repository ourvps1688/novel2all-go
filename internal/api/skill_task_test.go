package api

import (
	"errors"
	"testing"
	"time"
)

func TestSkillTaskManager_Start(t *testing.T) {
	m := NewSkillTaskManager()
	task := m.Start("story-long-write", "/p", map[string]any{"chapter": 1})
	if task.ID == "" {
		t.Error("task ID should not be empty")
	}
	if task.Name != "story-long-write" {
		t.Errorf("Name = %s", task.Name)
	}
	if task.Status != TaskStatusRunning {
		t.Errorf("Status = %s, want running", task.Status)
	}
}

func TestSkillTaskManager_Complete(t *testing.T) {
	m := NewSkillTaskManager()
	task := m.Start("skill1", "/p", nil)
	m.Complete(task.ID, "result")

	got, _ := m.Get(task.ID)
	if got.Status != TaskStatusCompleted {
		t.Errorf("Status = %s, want completed", got.Status)
	}
	if got.Output != "result" {
		t.Errorf("Output = %s, want result", got.Output)
	}
}

func TestSkillTaskManager_Fail(t *testing.T) {
	m := NewSkillTaskManager()
	task := m.Start("skill1", "/p", nil)
	m.Fail(task.ID, errors.New("boom"))

	got, _ := m.Get(task.ID)
	if got.Status != TaskStatusFailed {
		t.Errorf("Status = %s, want failed", got.Status)
	}
	if got.Err == nil {
		t.Error("Err should be set")
	}
}

func TestSkillTaskManager_List(t *testing.T) {
	m := NewSkillTaskManager()
	m.Start("a", "/p", nil)
	m.Start("b", "/p", nil)
	m.Start("c", "/p", nil)

	list := m.List()
	if len(list) != 3 {
		t.Errorf("List count = %d, want 3", len(list))
	}
}

func TestSkillTaskManager_Cleanup(t *testing.T) {
	m := NewSkillTaskManager()
	task := m.Start("s", "/p", nil)
	m.Complete(task.ID, "out")

	// 直接改 FinishedAt 模拟 1 小时前
	m.mu.Lock()
	m.tasks[task.ID].FinishedAt = ptrTime(time.Now().Add(-1 * time.Hour))
	m.mu.Unlock()

	m.CleanupOlderThan(30 * time.Minute)
	if _, ok := m.Get(task.ID); ok {
		t.Error("old task should be cleaned up")
	}
}
