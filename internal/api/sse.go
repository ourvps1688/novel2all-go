// Package api: sse.go — Sprint 28 SSE (Server-Sent Events) handlers.
//
// 对齐 Python V1 web/app.py:
//   - /api/write/stream       (chunk → progress → done)
//   - /api/write/active       (列出活跃 pipeline)
//   - /api/write/cancel/{tid} (取消任务)
//   - /api/skills/{name}/status (SSE skill 状态)
//   - /api/write/stream/model (流式 + 切 model)
//
// 前端用 EventSource API 订阅 chunk 流.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// writeSSEEvent 写一个 SSE event 到 w (格式: "event: <name>\ndata: <data>\n\n").
//
// 自动调 Flush() (需要 http.Flusher).
func writeSSEEvent(w http.ResponseWriter, event, data string) error {
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

// writeSSEJSON 写 SSE event (data 是 JSON 编码).
func writeSSEJSON(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeSSEEvent(w, event, string(data))
}

// setSSEHeaders 设置 SSE 响应头 (Content-Type / Cache-Control / Connection).
func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx 禁用 buffering
}

// handleWriteStream SSE 流式写作 (GET /api/write/stream).
//
// Query params:
//
//	chapter      (required) 章节号
//	project_root (default ".")
//	skill        (default "story-long-write")
//	min_chars    (default 2000)
//
// SSE 事件序列:
//   - started:    pipeline 启动
//   - chunk:      LLM 输出片段 (多次, 实时)
//   - progress:   阶段切换 (pre_write → write → post_write)
//   - post_write: post-write check 结果
//   - done:       完成 (含 output_path, char_count)
//   - error:      出错
func (h *WriteHandler) handleWriteStream(w http.ResponseWriter, r *http.Request) {
	if h.taskMgr == nil {
		http.Error(w, "task manager not initialized", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	chapter := parseIntQuery(q.Get("chapter"), 0)
	projectRoot := defaultStr(q.Get("project_root"), ".")
	skill := defaultStr(q.Get("skill"), "story-long-write")
	minChars := parseIntQuery(q.Get("min_chars"), 2000)

	if chapter <= 0 {
		http.Error(w, "chapter parameter required", http.StatusBadRequest)
		return
	}

	setSSEHeaders(w)
	flusher, _ := w.(http.Flusher)

	// 启动 task
	task := h.taskMgr.Start(r.Context(), chapter, projectRoot, skill)
	defer func() {
		// 如果客户端断开, 标记 cancelled
		if task.Status == TaskStatusRunning {
			_ = h.taskMgr.Cancel(task.ID)
		}
	}()

	// 1. started event
	_ = writeSSEJSON(w, "started", map[string]any{
		"task_id": task.ID,
		"chapter": task.Chapter,
		"skill":   task.Skill,
		"root":    task.ProjectRoot,
	})

	// 2. 同步运行 pipeline (V0 简化: 不真跑 LLM, mock chunk 流)
	// 真实实现会调 pipeline.Run() 并把 LLM chunk 推到 task.ChunkCh.
	// 这里 mock: 推 3 个 fake chunk + done event.
	mockChunks := []string{
		"这是第 1 个 chunk。",
		"继续写第 2 段内容，",
		"第 3 段结束。",
	}
	go func() {
		// progress: pre_write check
		_ = writeSSEJSON(w, "progress", map[string]any{
			"phase":  "pre_write_check",
			"status": "ok",
		})
		if flusher != nil {
			flusher.Flush()
		}
		// 推 chunks
		for _, chunk := range mockChunks {
			task.ChunkCh <- chunk
		}
		// progress: write 完成
		_ = writeSSEJSON(w, "progress", map[string]any{
			"phase":     "write",
			"status":    "completed",
			"min_chars": minChars,
		})
		if flusher != nil {
			flusher.Flush()
		}
		// progress: post_write check
		_ = writeSSEJSON(w, "post_write", map[string]any{
			"issues": []string{},
		})
		// 完成 task
		h.taskMgr.Complete(task.ID, "正文/第001章.md", 2500)
		_ = writeSSEJSON(w, "done", map[string]any{
			"output_path": "正文/第001章.md",
			"char_count":  2500,
		})
		if flusher != nil {
			flusher.Flush()
		}
		closeIfOpen(task.Done)
	}()

	// 3. 主循环: 从 task.ChunkCh 读 chunk + 推到 SSE
	for {
		select {
		case chunk, ok := <-task.ChunkCh:
			if !ok {
				return
			}
			if err := writeSSEJSON(w, "chunk", map[string]any{"text": chunk}); err != nil {
				return
			}
		case <-task.Done:
			return
		case <-r.Context().Done():
			// 客户端断开
			return
		}
	}
}

// handleWriteActive 列出活跃 pipeline (GET /api/write/active).
func (h *WriteHandler) handleWriteActive(w http.ResponseWriter, r *http.Request) {
	if h.taskMgr == nil {
		http.Error(w, "task manager not initialized", http.StatusServiceUnavailable)
		return
	}
	tasks := h.taskMgr.ListActive()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(tasks)
}

// handleWriteCancel 取消任务 (POST /api/write/cancel/{task_id}).
func (h *WriteHandler) handleWriteCancel(w http.ResponseWriter, r *http.Request) {
	if h.taskMgr == nil {
		http.Error(w, "task manager not initialized", http.StatusServiceUnavailable)
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/api/write/cancel/")
	taskID = strings.TrimSuffix(taskID, "/")
	if taskID == "" {
		http.Error(w, "task_id required", http.StatusBadRequest)
		return
	}
	if err := h.taskMgr.Cancel(taskID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled", "task_id": taskID})
}

// handleSkillStatus SSE skill 状态 (GET /api/skills/{name}/status).
//
// 用法: /api/skills/story-long-write/status?task_id=xxx
func (h *SkillsHandler) handleSkillStatus(w http.ResponseWriter, r *http.Request) {
	if h.skillTaskMgr == nil {
		http.Error(w, "skill task manager not initialized", http.StatusServiceUnavailable)
		return
	}
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		http.Error(w, "task_id query param required", http.StatusBadRequest)
		return
	}
	task, ok := h.skillTaskMgr.Get(taskID)
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	setSSEHeaders(w)
	flusher, _ := w.(http.Flusher)

	// 推当前状态
	_ = writeSSEJSON(w, "status", skillTaskToView(task))
	if flusher != nil {
		flusher.Flush()
	}

	// 如果已完成, 立即结束
	if task.Status != TaskStatusRunning {
		_ = writeSSEJSON(w, "done", map[string]any{
			"output": task.Output,
		})
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	// 否则每 1s 推一次更新 (V0 简化: 轮询)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			t, _ := h.skillTaskMgr.Get(taskID)
			if t == nil {
				return
			}
			_ = writeSSEJSON(w, "status", skillTaskToView(t))
			if flusher != nil {
				flusher.Flush()
			}
			if t.Status != TaskStatusRunning {
				_ = writeSSEJSON(w, "done", map[string]any{"output": t.Output})
				return
			}
		}
	}
}

// handleWriteStreamModel 流式 + 切 model (POST /api/write/stream/model).
//
// Body: {"chapter": N, "model": "...", "skill": "..."}
// SSE 事件序列同 handleWriteStream.
func (h *WriteHandler) handleWriteStreamModel(w http.ResponseWriter, r *http.Request) {
	if h.taskMgr == nil {
		http.Error(w, "task manager not initialized", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Chapter     int    `json:"chapter"`
		ProjectRoot string `json:"project_root"`
		Skill       string `json:"skill"`
		Model       string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Chapter <= 0 {
		http.Error(w, "chapter required", http.StatusBadRequest)
		return
	}
	if req.Skill == "" {
		req.Skill = "story-long-write"
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}

	setSSEHeaders(w)
	task := h.taskMgr.Start(r.Context(), req.Chapter, req.ProjectRoot, req.Skill)
	defer func() {
		if task.Status == TaskStatusRunning {
			_ = h.taskMgr.Cancel(task.ID)
		}
	}()

	_ = writeSSEJSON(w, "started", map[string]any{
		"task_id": task.ID,
		"chapter": task.Chapter,
		"model":   req.Model,
	})

	// 复用 mock 逻辑 (V0 简化)
	h.mockSSEWrite(w, r, task, 2000)
}

// mockSSEWrite mock 流式写作 (handleWriteStream + handleWriteStreamModel 共用).
//
// V0 简化: 推 3 个 fake chunk + done. 真实实现会调 writingPipeline.
func (h *WriteHandler) mockSSEWrite(w http.ResponseWriter, r *http.Request, task *PipelineTask, minChars int) {
	flusher, _ := w.(http.Flusher)
	mockChunks := []string{"chunk 1 ", "chunk 2 ", "chunk 3 done"}
	go func() {
		for _, chunk := range mockChunks {
			task.ChunkCh <- chunk
		}
		h.taskMgr.Complete(task.ID, "正文/第001章.md", 2500)
		_ = writeSSEJSON(w, "done", map[string]any{
			"output_path": "正文/第001章.md",
			"char_count":  2500,
			"min_chars":   minChars,
		})
		if flusher != nil {
			flusher.Flush()
		}
		closeIfOpen(task.Done)
	}()
	for {
		select {
		case chunk, ok := <-task.ChunkCh:
			if !ok {
				return
			}
			_ = writeSSEJSON(w, "chunk", map[string]any{"text": chunk})
		case <-task.Done:
			return
		case <-r.Context().Done():
			_ = writeSSEJSON(w, "cancelled", map[string]any{"task_id": task.ID})
			return
		}
	}
}

// helpers
func parseIntQuery(s string, def int) int {
	if s == "" {
		return def
	}
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
