// traces.go 提供 ring buffer 追踪记录器，用于 /debug/traces 端点。
//
// 设计：固定容量 ring buffer（默认 100），超容量时覆盖最旧事件。
// 线程安全（RWMutex 保护 buf/head/size）。
// 内存上限：~100 * sizeof(Trace) ≈ 几十 KB，开销可忽略。
package obs

import (
	"sync"
	"time"
)

// TraceType 追踪事件类型。
type TraceType string

const (
	// TraceHTTP HTTP 请求事件。
	TraceHTTP TraceType = "http"
	// TraceLLM LLM 调用事件。
	TraceLLM TraceType = "llm"
	// TraceError 错误事件（catch-all）。
	TraceError TraceType = "error"
)

// Trace 单条追踪事件（JSON 可序列化）。
//
// 字段按 type 区分使用：
//   - HTTP: Method/Path/Status/DurationMS
//   - LLM: Provider/Task/LLMStatus/DurationMS/TokensIn/TokensOut
//   - Error: Error
type Trace struct {
	Time       time.Time `json:"time"`
	Type       TraceType `json:"type"`
	Method     string    `json:"method,omitempty"`
	Path       string    `json:"path,omitempty"`
	Status     int       `json:"status,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`

	// LLM specific
	Provider  string `json:"provider,omitempty"`
	Task      string `json:"task,omitempty"`
	LLMStatus string `json:"llm_status,omitempty"`
	TokensIn  int    `json:"tokens_in,omitempty"`
	TokensOut int    `json:"tokens_out,omitempty"`

	// Error specific
	Error string `json:"error,omitempty"`
}

// TraceRecorder ring buffer 追踪记录器（线程安全）。
//
// 默认容量 100 条；容量 <= 0 时使用 100。
type TraceRecorder struct {
	mu       sync.RWMutex
	buf      []Trace
	capacity int
	head     int // 下一个写入位置
	size     int // 当前已填充数量（≤ capacity）
}

// NewTraceRecorder 创建 ring buffer。
func NewTraceRecorder(capacity int) *TraceRecorder {
	if capacity <= 0 {
		capacity = 100
	}
	return &TraceRecorder{
		buf:      make([]Trace, capacity),
		capacity: capacity,
	}
}

// Record 记录一条 trace（自动填 Time）。
func (r *TraceRecorder) Record(t Trace) {
	if t.Time.IsZero() {
		t.Time = time.Now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = t
	r.head = (r.head + 1) % r.capacity
	if r.size < r.capacity {
		r.size++
	}
}

// RecordHTTP 记录 HTTP 请求（便捷方法）。
func (r *TraceRecorder) RecordHTTP(method, path string, status int, duration time.Duration) {
	r.Record(Trace{
		Type:       TraceHTTP,
		Method:     method,
		Path:       path,
		Status:     status,
		DurationMS: duration.Milliseconds(),
	})
}

// RecordLLM 记录 LLM 调用（便捷方法）。
func (r *TraceRecorder) RecordLLM(provider, task, llmStatus string, duration time.Duration, tokensIn, tokensOut int) {
	r.Record(Trace{
		Type:       TraceLLM,
		Provider:   provider,
		Task:       task,
		LLMStatus:  llmStatus,
		DurationMS: duration.Milliseconds(),
		TokensIn:   tokensIn,
		TokensOut:  tokensOut,
	})
}

// RecordError 记录错误（nil 忽略）。
func (r *TraceRecorder) RecordError(err error) {
	if err == nil {
		return
	}
	r.Record(Trace{
		Type:  TraceError,
		Error: err.Error(),
	})
}

// Snapshot 返回所有 traces（按时间倒序：最新在前）。
//
// typeFilter 为空时返回所有类型；否则只返回匹配类型的 traces。
func (r *TraceRecorder) Snapshot(typeFilter TraceType) []Trace {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Trace, 0, r.size)
	for i := 0; i < r.size; i++ {
		// 从 head-1 开始倒序遍历（最新事件在 head-1）
		idx := (r.head - 1 - i + r.capacity) % r.capacity
		t := r.buf[idx]
		if typeFilter != "" && t.Type != typeFilter {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Size 返回当前 buffer 中的 trace 数。
func (r *TraceRecorder) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.size
}

// Capacity 返回 buffer 容量。
func (r *TraceRecorder) Capacity() int {
	return r.capacity
}

// Clear 清空 buffer（不释放内存，仅重置 head/size）。
func (r *TraceRecorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.head = 0
	r.size = 0
}
