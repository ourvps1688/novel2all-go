package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTaskManager_CreateAndGet(t *testing.T) {
	tm := NewTaskManager()
	ctx, cancel := func() (ctx interface{ Done() <-chan struct{} }, cancel func()) {
		c := make(chan struct{})
		return channelCtx{c}, func() { close(c) }
	}()
	_ = ctx
	task := tm.Create(1, "story-long-write", ".", 2000, false, cancel)
	if task.ID == "" {
		t.Fatal("task ID should be set")
	}
	got, ok := tm.Get(task.ID)
	if !ok {
		t.Fatal("Get should return the task")
	}
	if got.Chapter != 1 || got.Skill != "story-long-write" {
		t.Errorf("task fields wrong: %+v", got)
	}
}

func TestTaskManager_ListActive(t *testing.T) {
	tm := NewTaskManager()
	t1 := tm.Create(1, "story", ".", 100, false, func() {})
	t1.MarkRunning()
	t2 := tm.Create(2, "story", ".", 100, false, func() {})
	t2.MarkCompleted() // 已完成，不应算 active
	active := tm.ListActive()
	if len(active) != 1 {
		t.Errorf("active=%d, want 1 (only t1)", len(active))
	}
	if active[0].ID != t1.ID {
		t.Errorf("active[0].ID=%s, want %s", active[0].ID, t1.ID)
	}
}

func TestTaskManager_Cancel(t *testing.T) {
	tm := NewTaskManager()
	cancelled := false
	task := tm.Create(1, "story", ".", 100, false, func() { cancelled = true })
	if _, err := tm.Cancel(task.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !cancelled {
		t.Error("cancel callback not invoked")
	}
	if _, err := tm.Cancel("nonexistent"); err == nil {
		t.Error("cancel nonexistent should fail")
	}
}

func TestWriteHandler_CancelNotFound(t *testing.T) {
	h := NewWriteHandler(nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/write/cancel/nonexistent", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestWriteHandler_Active(t *testing.T) {
	h := NewWriteHandler(nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/write/active", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", rec.Code)
	}
}

func TestWriteHandler_Stream_MissingChapter(t *testing.T) {
	h := NewWriteHandler(nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/write/stream", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestWriteHandler_TaskNotFound(t *testing.T) {
	h := NewWriteHandler(nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/write/task/nonexistent", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestTrackingHandler(t *testing.T) {
	h := NewTrackingHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tracking", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", rec.Code)
	}
}

// channelCtx is a minimal context.Context-like for tests
type channelCtx struct {
	c chan struct{}
}

func (c channelCtx) Done() <-chan struct{} { return c.c }
func (c channelCtx) Deadline() (time.Time, bool) {
	return time.Time{}, false
}
func (c channelCtx) Value(key any) any { return nil }
