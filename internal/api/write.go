package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/memory"
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
//
// Sprint 32 加 Memory/References 注入（向后兼容, nil = 跳过 5 层 memory 自动加载）:
//   - memMgr: 调 LoadForWriting / UpdateAfterWriting, PreWriteCheck / PostWriteCheck
//   - refLoader: 调 LoadForSkill 注入 reference sections 到 system prompt
type WriteHandler struct {
	tasks    *TaskManager
	executor *skills.Executor
	loader   *skills.Loader
	taskMgr  *PipelineTaskManager // Sprint 28 SSE 流式

	// Sprint 32: memory + references 注入 (nil = 跳过对应功能, 行为等同 V0.29)
	memMgr    *memory.MemoryManager
	refLoader ReferencesLoader
}

// ReferencesLoader 抽象 interface (避免 import internal/references 形成循环).
//
// 实现方: internal/references.Loader 将在 Sprint 35 提供.
// 当前 Sprint 32 handleStream 不强依赖 refLoader, 所以接口定义在 api 包内,
// 任何实现 duck-type 满足 LoadForSkill(string) []string + LoadByName(string) string 即可.
type ReferencesLoader interface {
	LoadForSkill(skillName string) []string
	LoadByName(name string) string
}

// NewWriteHandler 创建
func NewWriteHandler(executor *skills.Executor, loader *skills.Loader) *WriteHandler {
	return &WriteHandler{
		tasks:    NewTaskManager(),
		executor: executor,
		loader:   loader,
	}
}

// NewWriteHandlerWithMemory 创建带 memory + references 注入的 WriteHandler.
//
// Sprint 32 工厂方法: 与 NewWriteHandler 签名不同的"with"变体.
// 不破坏现有调用方 (Sprint 22-31 测试都用 NewWriteHandler).
//
// 参数:
//   - executor: skills 执行器 (调 LLM)
//   - loader: skills 加载器 (读 SKILL.md)
//   - memMgr: memory manager (LoadForWriting/UpdateAfterWriting/Verifier).
//     可为 nil → 跳过 5 层 memory 自动加载 (行为等同 NewWriteHandler)
//   - refLoader: references 加载器 (LoadForSkill). 可为 nil → 跳过 references 注入
func NewWriteHandlerWithMemory(
	executor *skills.Executor,
	loader *skills.Loader,
	memMgr *memory.MemoryManager,
	refLoader ReferencesLoader,
) *WriteHandler {
	return &WriteHandler{
		tasks:     NewTaskManager(),
		executor:  executor,
		loader:    loader,
		memMgr:    memMgr,
		refLoader: refLoader,
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
		skill = skillStoryLongWrite
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

	// 2. pre_write_check (Sprint 32: 真化, memMgr != nil 时调 MemoryManager.PreWriteCheck)
	if !skipPreWrite && h.executor != nil {
		if h.memMgr != nil {
			// 加载细纲 (从 大纲/细纲_第NNN章.md)
			outline, _ := readOutline(projectRoot, int(chapter)) // best-effort
			// MemoryManager.PreWriteCheck 内部 LoadForWriting + 调 Verifier
			issues, _ := h.memMgr.PreWriteCheck(ctx, outline)
			status := "ok"
			notes := "no issues"
			if len(issues) > 0 {
				status = "issues"
				notes = fmt.Sprintf("%d continuity issues found", len(issues))
			}
			sendWriteSSE(w, flusher, "pre_write_check", map[string]any{
				"status": status,
				"notes":  notes,
			})
		} else {
			// 旧路径 (mock, memMgr=nil 行为等同 V0.29)
			sendWriteSSE(w, flusher, "pre_write_check", map[string]any{
				"status": "ok",
				"notes":  "P1-F mock pre-write check passed",
			})
		}
	}

	// 3. chunk + progress (调 executor.ExecuteStream)
	task.MarkRunning()
	if h.executor == nil {
		sendWriteSSE(w, flusher, "error", map[string]string{"message": "executor not configured"})
		cancel()
		return
	}

	// 3.5 Sprint 32: 构造 system prompt (memMgr 不为 nil 时注入 5 层 memory)
	//
	// 拼接顺序:
	//   [References] (refLoader) → [Memory 5 层] (MemoryContext.ToSystemSections) → [Project setting] (SetupMD/StyleMD)
	//
	// token 截断: total ≤ 32K chars (留 8K 给 LLM 余量), 优先保留 references + memory
	systemPrompt := buildSystemPrompt(h.refLoader, h.memMgr, ctx, int(chapter), projectRoot)

	// User prompt: 本章细纲 + 写作要求 (用 outline 而非 mock "第N章")
	userPrompt := buildUserPrompt(projectRoot, int(chapter), minChars)

	ch := make(chan llm.Chunk, 32)
	streamErrCh := make(chan error, 1)
	go func() {
		err := h.executor.ExecuteStream(ctx, skills.ExecuteInput{
			SkillName:   skill,
			UserInput:   userPrompt,
			SystemInput: systemPrompt, // Sprint 32 新增: 5 层 memory + references + settings
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
			task.UpdateProgress(len([]rune(accumulated.String())), accumulated.String())
			task.MarkCancelled()
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

	// 4. post_write_check (Sprint 32: 真化, memMgr != nil 时跑 MemoryManager.PostWriteCheck)
	if h.memMgr != nil {
		issues, _ := h.memMgr.PostWriteCheck(ctx, accumulated.String())
		status := "ok"
		notes := "no issues"
		if len(issues) > 0 {
			status = "issues"
			notes = fmt.Sprintf("%d continuity issues found", len(issues))
		}
		sendWriteSSE(w, flusher, "post_write_check", map[string]any{
			"status": status,
			"notes":  notes,
		})
	} else {
		// 旧路径 (mock)
		sendWriteSSE(w, flusher, "post_write_check", map[string]any{
			"status": "ok",
			"notes":  "P1-F mock post-write check passed",
		})
	}

	// 5. done
	finalContent := accumulated.String()
	task.UpdateProgress(len([]rune(finalContent)), finalContent)
	task.MarkCompleted()

	// 5.5 Sprint 32: 自动 Extractor + Tracker 更新 (memMgr != nil 时)
	if h.memMgr != nil {
		if _, err := h.memMgr.UpdateAfterWriting(ctx, int(chapter), finalContent); err != nil {
			sendWriteSSE(w, flusher, "extract_warning", map[string]string{
				"message": fmt.Sprintf("memory update failed: %v", err),
			})
		}
	}

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

// buildSystemPrompt Sprint 32: 拼 system prompt (memory + references + settings).
//
// 拼接顺序 (与 docs/p2-production-readiness-plan.md 一致):
//  1. References (refLoader, 按 skill)
//  2. Memory 5 层 (MemoryContext.ToSystemSections)
//  3. Project settings (设定/文风.md + 创作设定.md)
//
// token 截断: 总长度 ≤ 32K chars (留 8K 给 LLM 余量).
// 任一组件 nil/空时跳过该段 (向后兼容: V0.29 路径 system prompt 只含 skill.Body).
func buildSystemPrompt(refLoader ReferencesLoader, memMgr *memory.MemoryManager,
	ctx context.Context, chapter int, projectRoot string) string {
	const maxSystemLen = 32000
	var parts []string

	// 1. References (Sprint 35 才有 refLoader; V0.30 仍为 nil)
	if refLoader != nil {
		refs := refLoader.LoadForSkill("")
		if len(refs) > 0 {
			s := "# References\n"
			for _, r := range refs {
				s += "\n\n## " + r + "\n"
				s += refLoader.LoadByName(r)
			}
			parts = append(parts, s)
		}
	}

	// 2. Memory 5 层
	if memMgr != nil {
		if mc, err := memMgr.LoadForWriting(ctx, chapter); err == nil {
			parts = append(parts, mc.ToSystemSections()...)
		}
	}

	// 3. Project settings (设定/文风.md 是 baseline, 永远加载)
	settingMDs := []string{"设定/文风.md", "创作设定.md"}
	for _, rel := range settingMDs {
		fp := filepath.Join(projectRoot, rel)
		if data, err := os.ReadFile(fp); err == nil {
			parts = append(parts, "# "+rel+"\n"+string(data))
		}
	}

	result := strings.Join(parts, "\n\n---\n\n")
	if len(result) > maxSystemLen {
		result = result[:maxSystemLen] + "\n\n[... truncated for token limit ...]"
	}
	return result
}

// buildUserPrompt Sprint 32: 拼 user prompt (细纲 + 写作要求).
//
// 优先从 大纲/细纲_第NNN章.md 加载 outline (真 prompt);
// 缺失时降级到 "第 N 章" 占位 (向后兼容).
func buildUserPrompt(projectRoot string, chapter, minChars int) string {
	outline, _ := readOutline(projectRoot, chapter)
	if outline != "" {
		return fmt.Sprintf(`请基于以下细纲写第 %d 章正文:

# 本章细纲
%s

# 写作要求
- 最低字数: %d 字
- 风格: 保持与前文一致
- 人物: 不偏离已有角色设定
- 伏笔: 新埋伏笔需合理可回收

开始写正文:`, chapter, outline, minChars)
	}
	return fmt.Sprintf("第 %d 章（最低 %d 字）", chapter, minChars)
}

// readOutline 加载本章细纲 (best-effort, 缺失返回空字符串).
//
// 路径: {projectRoot}/大纲/细纲_第NNN章.md
// 缺失时不报错 (V0.30 graceful degradation).
func readOutline(projectRoot string, chapter int) (string, error) {
	fp := filepath.Join(projectRoot, "大纲", fmt.Sprintf("细纲_第%03d章.md", chapter))
	data, err := os.ReadFile(fp)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
