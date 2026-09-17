package obs

import (
	"sync"
	"testing"
	"time"
)

func TestTraceRecorder_NewDefaultCapacity(t *testing.T) {
	r := NewTraceRecorder(0)
	if r.Capacity() != 100 {
		t.Errorf("expected default capacity=100, got %d", r.Capacity())
	}
	r2 := NewTraceRecorder(-5)
	if r2.Capacity() != 100 {
		t.Errorf("expected default capacity=100 for negative input, got %d", r2.Capacity())
	}
}

func TestTraceRecorder_RecordAndSnapshot(t *testing.T) {
	r := NewTraceRecorder(10)

	r.RecordHTTP("GET", "/health", 200, 5*time.Millisecond)
	r.RecordHTTP("POST", "/api/auth/login", 401, 12*time.Millisecond)

	snap := r.Snapshot("")
	if len(snap) != 2 {
		t.Fatalf("expected 2 traces, got %d", len(snap))
	}
	// 最新事件在前
	if snap[0].Method != "POST" {
		t.Errorf("expected first trace to be latest (POST), got %q", snap[0].Method)
	}
	if snap[1].Method != "GET" {
		t.Errorf("expected second trace to be older (GET), got %q", snap[1].Method)
	}
	if snap[0].Type != TraceHTTP {
		t.Errorf("expected type=http, got %q", snap[0].Type)
	}
}

func TestTraceRecorder_RingBufferOverflow(t *testing.T) {
	r := NewTraceRecorder(3)

	for i := 0; i < 5; i++ {
		r.RecordHTTP("GET", "/path", 200+i, time.Duration(i)*time.Millisecond)
		time.Sleep(time.Millisecond) // 保证 Time 递增
	}

	if r.Size() != 3 {
		t.Errorf("expected size=3 (capacity), got %d", r.Size())
	}

	snap := r.Snapshot("")
	if len(snap) != 3 {
		t.Fatalf("expected 3 traces in snapshot, got %d", len(snap))
	}
	// 最新事件是 status=204（i=4）
	if snap[0].Status != 204 {
		t.Errorf("expected newest status=204, got %d", snap[0].Status)
	}
	// 最旧保留的是 status=202（i=2）
	if snap[2].Status != 202 {
		t.Errorf("expected oldest retained status=202, got %d", snap[2].Status)
	}
}

func TestTraceRecorder_FilterByType(t *testing.T) {
	r := NewTraceRecorder(10)

	r.RecordHTTP("GET", "/health", 200, time.Millisecond)
	r.RecordLLM("minimax", "WRITING", "success", 100*time.Millisecond, 50, 200)
	r.RecordHTTP("POST", "/api/auth/login", 401, time.Millisecond)
	r.RecordLLM("deepseek", "CONSISTENCY", "success", 80*time.Millisecond, 30, 100)
	r.RecordError(errTest("something broke"))

	httpOnly := r.Snapshot(TraceHTTP)
	if len(httpOnly) != 2 {
		t.Errorf("expected 2 HTTP traces, got %d", len(httpOnly))
	}
	for _, tr := range httpOnly {
		if tr.Type != TraceHTTP {
			t.Errorf("filter leaked non-HTTP trace: %q", tr.Type)
		}
	}

	llmOnly := r.Snapshot(TraceLLM)
	if len(llmOnly) != 2 {
		t.Errorf("expected 2 LLM traces, got %d", len(llmOnly))
	}
	for _, tr := range llmOnly {
		if tr.Type != TraceLLM {
			t.Errorf("filter leaked non-LLM trace: %q", tr.Type)
		}
	}

	errOnly := r.Snapshot(TraceError)
	if len(errOnly) != 1 {
		t.Errorf("expected 1 error trace, got %d", len(errOnly))
	}
	if errOnly[0].Error != "something broke" {
		t.Errorf("expected error message preserved, got %q", errOnly[0].Error)
	}

	all := r.Snapshot("")
	if len(all) != 5 {
		t.Errorf("expected all 5 traces, got %d", len(all))
	}
}

func TestTraceRecorder_RecordError_NilIgnored(t *testing.T) {
	r := NewTraceRecorder(10)
	r.RecordError(nil)
	if r.Size() != 0 {
		t.Errorf("expected size=0 after nil error, got %d", r.Size())
	}
}

func TestTraceRecorder_Clear(t *testing.T) {
	r := NewTraceRecorder(10)
	r.RecordHTTP("GET", "/health", 200, time.Millisecond)
	r.RecordHTTP("GET", "/health", 200, time.Millisecond)
	if r.Size() != 2 {
		t.Fatalf("expected size=2, got %d", r.Size())
	}
	r.Clear()
	if r.Size() != 0 {
		t.Errorf("expected size=0 after Clear, got %d", r.Size())
	}
	if len(r.Snapshot("")) != 0 {
		t.Errorf("expected empty snapshot after Clear")
	}
}

func TestTraceRecorder_AutoFillTime(t *testing.T) {
	r := NewTraceRecorder(10)
	before := time.Now()
	r.Record(Trace{Type: TraceHTTP, Method: "GET", Path: "/health", Status: 200})
	after := time.Now()

	snap := r.Snapshot("")
	if snap[0].Time.Before(before) || snap[0].Time.After(after) {
		t.Errorf("expected Time to be auto-filled in [%v, %v], got %v", before, after, snap[0].Time)
	}
}

func TestTraceRecorder_ConcurrentRecord(t *testing.T) {
	r := NewTraceRecorder(100)
	var wg sync.WaitGroup
	const goroutines = 20
	const iterations = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				r.RecordHTTP("GET", "/health", 200, time.Millisecond)
			}
		}()
	}
	wg.Wait()

	// buffer 满（100），不会超过 capacity
	if r.Size() != 100 {
		t.Errorf("expected size=100 (capacity), got %d", r.Size())
	}
	if len(r.Snapshot("")) != 100 {
		t.Errorf("expected 100 traces in snapshot, got %d", len(r.Snapshot("")))
	}
}

func TestTraceRecorder_PreservesProvidedTime(t *testing.T) {
	r := NewTraceRecorder(10)
	custom := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	r.Record(Trace{Type: TraceError, Time: custom, Error: "boom"})

	snap := r.Snapshot("")
	if !snap[0].Time.Equal(custom) {
		t.Errorf("expected Time=%v, got %v", custom, snap[0].Time)
	}
}

// errTest 简单的 error 实现用于测试。
type errTest string

func (e errTest) Error() string { return string(e) }
