package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// WriteHandler 提供 /api/write/* + /api/tracking
//
// P1-F 切片 3：流式写作基础设施
//   - GET  /api/write/stream        → SSE 流式生成章节
//   - POST /api/write/cancel/{id}   → 取消任务
//   - GET  /api/write/active        → 列出活跃任务
//   - GET  /api/write/task/{id}     → 取单个任务状态
//   - GET  /api/tracking            → 项目状态（mock）
type WriteHandler struct {
	tasks    *TaskManager
	executor *skills.Executor
	loader   *skills.Loader
	taskMgr  *PipelineTaskManager // Sprint 28 SSE 流式
}

// NewWriteHandler 创建
func NewWriteHandler(executor *skills.Executor, loader *skills.Loader) *WriteHandler {
	return &WriteHandler{
		tasks:    NewTaskManager(),
		executor: executor,
		loader:   loader,
	}
}

// Tasks 暴露 TaskManager（给 tracking 等其他模块查询）
func (h *WriteHandler) Tasks() *TaskManager { return h.tasks }

// ServeHTTP 路由分发
func (h *WriteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/write/")
	path = strings.Trim(path, "/")

	switch {
	case path == "stream":
		// GET /api/write/stream (SSE)
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleStream(w, r)
	case path == "active":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleActive(w, r)
	case strings.HasPrefix(path, "cancel/"):
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := strings.TrimPrefix(path, "cancel/")
		h.handleCancel(w, r, taskID)
	case strings.HasPrefix(path, "task/"):
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := strings.TrimPrefix(path, "task/")
		h.handleTask(w, r, taskID)
	default:
		http.NotFound(w, r)
	}
}

// handleStream SSE 流式生成
//
// Query params:
//   - chapter    (int, required) 章节号
//   - project_root (string) 项目目录
//   - skill      (string) skill 名，默认 story-long-write
//   - min_chars  (int) 最低字数
//   - skip_pre_write (bool) 跳过 pre-write check
//
// SSE 事件序列：
//   - started: 任务启动
//   - pre_write_check: pre-write check（mock）
//   - chunk: LLM 输出片段（多次）
//   - progress: 进度更新
//   - post_write_check: post-write check（mock）
//   - done: 完成（含 output_path, content_chars, task_id）
//   - cancelled: 取消
//   - error: 失败
//
//nolint:gocyclo // SSE handler 天然多分支（started/pre-check/chunks/progress/done/error/cancel）
func (h *WriteHandler) handleStream(w http.ResponseWriter, r *http.Request) {
	// 解析 query
	chapter, err := parseInt64(r.URL.Query().Get("chapter"))
	if err != nil || chapter <= 0 {
		http.Error(w, `{"error":"missing or invalid 'chapter'"}`, http.StatusBadRequest)
		return
	}
	skill := r.URL.Query().Get("skill")
	if skill == "" {
		skill = "story-long-write"
	}
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		projectRoot = "."
	}
	minCharsStr := r.URL.Query().Get("min_chars")
	minChars := 2000
	if minCharsStr != "" {
		if v, err := parseInt64(minCharsStr); err == nil {
			minChars = int(v)
		}
	}
	skipPreWrite := r.URL.Query().Get("skip_pre_write") == "true"

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 创建任务 + ctx
	ctx, cancel := context.WithCancel(r.Context())
	task := h.tasks.Create(int(chapter), skill, projectRoot, minChars, skipPreWrite, cancel)

	// 1. started
	sendWriteSSE(w, flusher, "started", map[string]any{
		"task_id":        task.ID,
		"chapter":        task.Chapter,
		"skill":          task.Skill,
		"min_chars":      task.MinChars,
		"skip_pre_write": task.SkipPreWrite,
		"project_root":   task.ProjectRoot,
	})

	// 2. pre_write_check (mock)
	if !skipPreWrite && h.executor != nil {
		sendWriteSSE(w, flusher, "pre_write_check", map[string]any{
			"status": "ok",
			"notes":  "P1-F mock pre-write check passed",
		})
	}

	// 3. chunk + progress (调 executor.ExecuteStream)
	task.MarkRunning()
	if h.executor == nil {
		sendWriteSSE(w, flusher, "error", map[string]string{"message": "executor not configured"})
		cancel()
		return
	}

	ch := make(chan llm.Chunk, 32)
	streamErrCh := make(chan error, 1)
	go func() {
		err := h.executor.ExecuteStream(ctx, skills.ExecuteInput{
			SkillName: skill,
			UserInput: fmt.Sprintf("第 %d 章", chapter),
			Variables: map[string]any{
				"chapter":   chapter,
				"min_chars": minChars,
				"project":   projectRoot,
			},
		}, ch)
		streamErrCh <- err
	}()

	var accumulated strings.Builder
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	streamDone := false
	for !streamDone {
		select {
		case <-ctx.Done():
			// 客户端断开 / 取消
			task.MarkCancelled()
			task.Content = accumulated.String()
			task.CharsWritten = len([]rune(accumulated.String()))
			sendWriteSSE(w, flusher, "cancelled", map[string]any{
				"task_id":         task.ID,
				"chars_so_far":    task.CharsWritten,
				"partial_content": task.Content,
			})
			return
		case chunk, ok := <-ch:
			if !ok {
				streamDone = true
				break
			}
			if chunk.Err != nil {
				task.MarkFailed()
				sendWriteSSE(w, flusher, "error", map[string]string{
					"message": chunk.Err.Error(),
				})
				return
			}
			if chunk.Content != "" {
				accumulated.WriteString(chunk.Content)
				sendWriteSSE(w, flusher, "chunk", map[string]any{
					"content": chunk.Content,
				})
			}
			if chunk.Done {
				streamDone = true
			}
		case <-ticker.C:
			// 定期 progress
			sendWriteSSE(w, flusher, "progress", map[string]any{
				"chars_so_far": len([]rune(accumulated.String())),
				"target_chars": minChars,
			})
		}
	}

	// 等 goroutine 真正完成
	if err := <-streamErrCh; err != nil {
		task.MarkFailed()
		sendWriteSSE(w, flusher, "error", map[string]string{"message": err.Error()})
		return
	}

	// 4. post_write_check (mock)
	sendWriteSSE(w, flusher, "post_write_check", map[string]any{
		"status": "ok",
		"notes":  "P1-F mock post-write check passed",
	})

	// 5. done
	task.Content = accumulated.String()
	task.CharsWritten = len([]rune(accumulated.String()))
	task.MarkCompleted()

	sendWriteSSE(w, flusher, "done", map[string]any{
		"task_id":       task.ID,
		"chapter":       task.Chapter,
		"content_chars": task.CharsWritten,
		"output_path":   fmt.Sprintf("%s/chapter_%03d.md", projectRoot, chapter),
		"content":       task.Content,
	})
}

// handleActive 列出活跃任务
func (h *WriteHandler) handleActive(w http.ResponseWriter, _ *http.Request) {
	active := h.tasks.ListActive()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"active": active,
		"count":  len(active),
	})
}

// handleCancel 取消任务
func (h *WriteHandler) handleCancel(w http.ResponseWriter, _ *http.Request, taskID string) {
	t, err := h.tasks.Cancel(taskID)
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			http.Error(w, fmt.Sprintf(`{"error":"task %s not found"}`, taskID), http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrTaskDone) {
			http.Error(w, fmt.Sprintf(`{"error":"task %s already done","status":"done"}`, taskID), http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(t)
}

// handleTask 取单个任务
func (h *WriteHandler) handleTask(w http.ResponseWriter, _ *http.Request, taskID string) {
	t, ok := h.tasks.Get(taskID)
	if !ok {
		http.Error(w, fmt.Sprintf(`{"error":"task %s not found"}`, taskID), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(t)
}

// sendWriteSSE 发送一条写章节相关的 SSE 事件（与 skills.go sendSSE 不同 schema）
func sendWriteSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		jsonData = []byte(`{"error":"marshal failed"}`)
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
	flusher.Flush()
}
