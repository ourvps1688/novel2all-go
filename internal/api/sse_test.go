package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/skills"
)

func newTestWriteHandler() *WriteHandler {
	// 用空 Loader + nil Router (不真跑 LLM)
	loader, _ := skills.NewLoader()
	exec := skills.NewExecutor(loader, nil)
	return &WriteHandler{
		tasks:    nil,
		executor: exec,
		loader:   loader,
		taskMgr:  NewPipelineTaskManager(),
	}
}

func TestHandleWriteActive_NoTaskManager(t *testing.T) {
	// 没有 taskMgr 时返 503
	h := &WriteHandler{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/active", http.NoBody)
	h.handleWriteActive(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleWriteActive_Empty(t *testing.T) {
	h := newTestWriteHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/active", http.NoBody)
	h.handleWriteActive(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "[]") {
		t.Errorf("expected empty array, got %s", w.Body.String())
	}
}

func TestHandleWriteActive_WithTasks(t *testing.T) {
	h := newTestWriteHandler()
	// 注册 2 个任务, 1 个完成 1 个 running
	t1 := h.taskMgr.Start(testCtx(), 1, "/p", "s1")
	t2 := h.taskMgr.Start(testCtx(), 2, "/p", "s2")
	h.taskMgr.Complete(t2.ID, "/o", 100)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/active", http.NoBody)
	h.handleWriteActive(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, t1.ID) {
		t.Errorf("expected t1 in body, got %s", body)
	}
	if strings.Contains(body, t2.ID) {
		t.Errorf("completed task t2 should NOT appear in active list")
	}
}

func TestHandleWriteCancel_Success(t *testing.T) {
	h := newTestWriteHandler()
	task := h.taskMgr.Start(testCtx(), 1, "/p", "s")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/write/cancel/"+task.ID, http.NoBody)
	h.handleWriteCancel(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"cancelled"`) {
		t.Errorf("expected cancelled status, got %s", w.Body.String())
	}
}

func TestHandleWriteCancel_NotFound(t *testing.T) {
	h := newTestWriteHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/write/cancel/nonexistent", http.NoBody)
	h.handleWriteCancel(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleWriteCancel_NoTaskID(t *testing.T) {
	h := newTestWriteHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/write/cancel/", http.NoBody)
	h.handleWriteCancel(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleWriteStream_NoTaskMgr(t *testing.T) {
	h := &WriteHandler{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/stream?chapter=1", http.NoBody)
	h.handleWriteStream(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleWriteStream_MissingChapter(t *testing.T) {
	h := newTestWriteHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/stream", http.NoBody)
	h.handleWriteStream(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleWriteStream_SSEFormat(t *testing.T) {
	h := newTestWriteHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/stream?chapter=1", http.NoBody)

	// 用 goroutine 跑 handler (因为 handler 会 block 直到 task.Done)
	done := make(chan struct{})
	go func() {
		h.handleWriteStream(w, r)
		close(done)
	}()

	// 等待所有 events 推送完成
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleWriteStream did not complete in 2s")
	}

	// 验证 SSE headers
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", w.Header().Get("Content-Type"))
	}

	// 解析 SSE events
	body := w.Body.String()
	scanner := bufio.NewScanner(strings.NewReader(body))
	events := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			events = append(events, strings.TrimPrefix(line, "event: "))
		}
	}
	// V0 mock 流式: 顺序是 started → progress → chunks → done (但由于 chunks
	// 和 done 在不同 goroutine 推送, 实际顺序可能是 started → chunks → done)
	if len(events) == 0 {
		t.Fatalf("no SSE events found in body: %s", body)
	}
	if events[0] != "started" {
		t.Errorf("first event = %s, want started", events[0])
	}
	// 验证关键事件都出现过 (顺序不限)
	foundStarted, foundChunk, foundDone := false, false, false
	for _, e := range events {
		switch e {
		case "started":
			foundStarted = true
		case "chunk":
			foundChunk = true
		case "done":
			foundDone = true
		}
	}
	if !foundStarted || !foundChunk || !foundDone {
		t.Errorf("missing events: started=%v chunk=%v done=%v (all: %v)",
			foundStarted, foundChunk, foundDone, events)
	}
}

func TestHandleWriteStreamModel_BadJSON(t *testing.T) {
	h := newTestWriteHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/write/stream/model",
		strings.NewReader("not json"))
	h.handleWriteStreamModel(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleWriteStreamModel_MissingChapter(t *testing.T) {
	h := newTestWriteHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/write/stream/model",
		strings.NewReader(`{"skill": "x"}`))
	h.handleWriteStreamModel(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSkillStatus_NoMgr(t *testing.T) {
	h := &SkillsHandler{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/skills/x/status?task_id=t", http.NoBody)
	h.handleSkillStatus(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleSkillStatus_NoTaskID(t *testing.T) {
	h := &SkillsHandler{skillTaskMgr: NewSkillTaskManager()}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/skills/x/status", http.NoBody)
	h.handleSkillStatus(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSkillStatus_TaskNotFound(t *testing.T) {
	h := &SkillsHandler{skillTaskMgr: NewSkillTaskManager()}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/skills/x/status?task_id=nonexistent", http.NoBody)
	h.handleSkillStatus(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleSkillStatus_Completed(t *testing.T) {
	mgr := NewSkillTaskManager()
	task := mgr.Start("test", "/p", nil)
	mgr.Complete(task.ID, "output-result")
	h := &SkillsHandler{skillTaskMgr: mgr}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/skills/test/status?task_id="+task.ID, http.NoBody)
	h.handleSkillStatus(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected SSE content type, got %s", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), `"output":"output-result"`) {
		t.Errorf("expected output in body, got %s", w.Body.String())
	}
}

// helpers
func testCtx() context.Context { return context.Background() }
