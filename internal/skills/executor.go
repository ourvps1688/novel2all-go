package skills

import (
	"context"
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// Executor 负责执行 skill：加载 + 拼 prompt + 调 LLM
type Executor struct {
	loader *Loader
	router *llm.Router
}

// NewExecutor 创建 executor
func NewExecutor(loader *Loader, router *llm.Router) *Executor {
	return &Executor{loader: loader, router: router}
}

// Execute 非流式执行 skill
func (e *Executor) Execute(ctx context.Context, input ExecuteInput) (*ExecuteResult, error) {
	skill, err := e.loader.Get(input.SkillName)
	if err != nil {
		return nil, err
	}

	task := llm.TaskUnknown
	if input.Task != "" {
		task = llm.TaskType(input.Task)
	}
	req := llm.Request{
		Task: task,
		Messages: []llm.Message{
			{Role: "system", Content: skill.Body},
			{Role: "user", Content: input.UserInput},
		},
		Stream:           false,
		OverrideProvider: llm.ProviderName(input.Provider),
		OverrideModel:    input.Model,
	}

	resp, err := e.router.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", input.SkillName, err)
	}

	return &ExecuteResult{
		Content:   resp.Content,
		Provider:  string(resp.Provider),
		Model:     resp.Model,
		TokensIn:  resp.TokensIn,
		TokensOut: resp.TokensOut,
	}, nil
}

// ExecuteStream 流式执行 skill
// ch 会在结束时被关闭
func (e *Executor) ExecuteStream(ctx context.Context, input ExecuteInput, ch chan<- llm.Chunk) error {
	skill, err := e.loader.Get(input.SkillName)
	if err != nil {
		return err
	}

	task := llm.TaskUnknown
	if input.Task != "" {
		task = llm.TaskType(input.Task)
	}
	req := llm.Request{
		Task: task,
		Messages: []llm.Message{
			{Role: "system", Content: skill.Body},
			{Role: "user", Content: input.UserInput},
		},
		Stream:           true,
		OverrideProvider: llm.ProviderName(input.Provider),
		OverrideModel:    input.Model,
	}

	return e.router.ChatStream(ctx, req, ch)
}

// ExecuteWithTask 显式指定 task type（用于需要 WRITING 等路由的场景）
// TaskType 控制 router 选 provider（WRITING→minimax / 其他→deepseek）
func (e *Executor) ExecuteWithTask(ctx context.Context, input ExecuteInput, task llm.TaskType, ch chan<- llm.Chunk) error {
	skill, err := e.loader.Get(input.SkillName)
	if err != nil {
		return err
	}

	req := llm.Request{
		Task: task,
		Messages: []llm.Message{
			{Role: "system", Content: skill.Body},
			{Role: "user", Content: input.UserInput},
		},
		Stream: true,
	}

	return e.router.ChatStream(ctx, req, ch)
}
