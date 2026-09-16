package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// SkillsHandler 暴露 /api/skills 路由
type SkillsHandler struct {
	executor *skills.Executor
	loader   *skills.Loader
}

// NewSkillsHandler 创建 handler
func NewSkillsHandler(executor *skills.Executor, loader *skills.Loader) *SkillsHandler {
	return &SkillsHandler{executor: executor, loader: loader}
}

// SkillsListResponse GET /api/skills 响应
type SkillsListResponse struct {
	Skills []SkillSummary `json:"skills"`
	Count  int            `json:"count"`
}

// SkillSummary 单个 skill 简要
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ExecuteRequest POST /api/skills/{name}/execute 请求体
type ExecuteRequest struct {
	Input     string         `json:"input"`
	Task      string         `json:"task,omitempty"`     // 可选：覆盖 task type
	Provider  string         `json:"provider,omitempty"` // 可选：覆盖 provider
	Model     string         `json:"model,omitempty"`    // 可选：覆盖 model
	Variables map[string]any `json:"variables,omitempty"`
}

// SSEEvent 流式响应事件（与 Python V1.5.5 兼容）
type SSEEvent struct {
	Event   string `json:"event"`             // "started" | "chunk" | "progress" | "done" | "error"
	Content string `json:"content,omitempty"` // chunk 内容
	Done    bool   `json:"done,omitempty"`
	Error   string `json:"error,omitempty"`
	Meta    any    `json:"meta,omitempty"` // provider/model 等元数据
}

// ServeHTTP 实现路由分发
//
//	GET  /api/skills                      → 列出所有
//	POST /api/skills/{name}/execute       → 流式执行
//	POST /api/skills/{name}/execute-sync  → 同步执行
func (h *SkillsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/skills")
	path = strings.Trim(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		h.list(w, r)
	case path == "" && r.Method == http.MethodPost:
		http.Error(w, "skill name required", http.StatusBadRequest)
	default:
		// /{name}/execute 或 /{name}/execute-sync
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		name := parts[0]
		action := parts[1]
		switch action {
		case "execute":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.executeStream(w, r, name)
		case "execute-sync":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.executeSync(w, r, name)
		default:
			http.NotFound(w, r)
		}
	}
}

func (h *SkillsHandler) list(w http.ResponseWriter, r *http.Request) {
	all := h.loader.ListDetailed()
	out := SkillsListResponse{Skills: make([]SkillSummary, 0, len(all)), Count: len(all)}
	for _, s := range all {
		out.Skills = append(out.Skills, SkillSummary{
			Name:        s.Name,
			Description: s.Description,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *SkillsHandler) executeStream(w http.ResponseWriter, r *http.Request, name string) {
	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Input) == "" {
		http.Error(w, "input is required", http.StatusBadRequest)
		return
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// 拼 LLM request
	llmReq := llm.Request{
		Task: llm.TaskUnknown,
		Messages: []llm.Message{
			{Role: "system", Content: ""}, // 占位，下面覆盖
			{Role: "user", Content: req.Input},
		},
		Stream:           true,
		OverrideProvider: llm.ProviderName(req.Provider),
		OverrideModel:    req.Model,
	}
	if req.Task != "" {
		llmReq.Task = llm.TaskType(req.Task)
	}

	// 加载 skill body
	skill, err := h.loader.Get(name)
	if err != nil {
		sendSSE(w, flusher, SSEEvent{Event: "error", Error: err.Error()})
		return
	}
	llmReq.Messages[0].Content = skill.Body

	// 发送 started 事件
	sendSSE(w, flusher, SSEEvent{Event: "started", Meta: map[string]string{
		"skill": name, "task": string(llmReq.Task),
	}})

	// 流式调用
	ch := make(chan llm.Chunk, 32)
	errCh := make(chan error, 1)
	go func() {
		err := h.executor.ExecuteStream(ctx, skills.ExecuteInput{
			SkillName: name,
			UserInput: req.Input,
			Variables: req.Variables,
		}, ch)
		errCh <- err
	}()

	// 转发 chunk 到 SSE
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				ch = nil
				continue
			}
			if chunk.Err != nil {
				sendSSE(w, flusher, SSEEvent{Event: "error", Error: chunk.Err.Error()})
				return
			}
			if chunk.Done {
				sendSSE(w, flusher, SSEEvent{Event: "done", Done: true})
				return
			}
			if chunk.Content != "" {
				sendSSE(w, flusher, SSEEvent{Event: "chunk", Content: chunk.Content})
			}
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				sendSSE(w, flusher, SSEEvent{Event: "error", Error: err.Error()})
			}
			return
		case <-ctx.Done():
			sendSSE(w, flusher, SSEEvent{Event: "error", Error: "timeout"})
			return
		}
	}
}

func (h *SkillsHandler) executeSync(w http.ResponseWriter, r *http.Request, name string) {
	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	llmReq := llm.Request{
		Task: llm.TaskUnknown,
		Messages: []llm.Message{
			{Role: "user", Content: req.Input},
		},
		Stream:           false,
		OverrideProvider: llm.ProviderName(req.Provider),
		OverrideModel:    req.Model,
	}
	if req.Task != "" {
		llmReq.Task = llm.TaskType(req.Task)
	}

	skill, err := h.loader.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	llmReq.Messages = append([]llm.Message{{Role: "system", Content: skill.Body}}, llmReq.Messages...)

	// 直接调 executor
	result, err := h.executor.Execute(ctx, skills.ExecuteInput{
		SkillName: name,
		UserInput: req.Input,
		Variables: req.Variables,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, ev SSEEvent) {
	data, _ := json.Marshal(ev)
	_, _ = io.WriteString(w, "data: ")
	_, _ = w.Write(data)
	_, _ = io.WriteString(w, "\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
