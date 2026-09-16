package skills

import (
	"context"
	"errors"
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

	req := llm.Request{
		Task: llm.TaskUnknown, // executor 不预设 task，由 router 默认 deepseek
		Messages: []llm.Message{
			{Role: "system", Content: skill.Body},
			{Role: "user", Content: input.UserInput},
		},
		Stream: false,
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

	req := llm.Request{
		Task: llm.TaskUnknown,
		Messages: []llm.Message{
			{Role: "system", Content: skill.Body},
			{Role: "user", Content: input.UserInput},
		},
		Stream: true,
	}

	return e.router.ChatStream(ctx, req, ch)
}

// ExecuteWithTask 显式指定 task type（用于需要 WRITING 等路由的场景）
func (e *Executor) ExecuteWithTask(ctx context.Context, input ExecuteInput, task llm.TaskType) error {
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

	ch := make(chan llm.Chunk, 32)
	go func() {
		_ = e.router.ChatStream(ctx, req, ch)
	}()

	// 流式结果由调用方消费 ch
	// （这个函数语义待定，本版本暂用 ExecuteStream）
	for range ch {
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		// 防止 nil deref
	}
	_ = skill
	return nil
}