package agent

import (
	"context"
	"fmt"
	"sync"
)

// Orchestrator 多 agent 编排器 (Sprint A5.4)
//
// 支持两种模式：
//   - Sequential：按顺序串行执行（agent1 → agent2 → ...）
//   - Parallel：并发执行（sync.WaitGroup + error 聚合）
//
// 每个 Step 包含：agent 名 + 用户输入 + 共享上下文。
//
// 输出：每个 step 的 AgentResult + 总错误。
type Orchestrator struct {
	dispatcher *Dispatcher
}

// NewOrchestrator 构造
func NewOrchestrator(d *Dispatcher) *Orchestrator {
	return &Orchestrator{dispatcher: d}
}

// Step 编排步骤
type Step struct {
	AgentName string // vendor 原名（如 "story-architect"）
	UserInput string // 该 agent 的 user input
}

// StepResult 步骤执行结果
type StepResult struct {
	Step   Step
	Output string // agent 返回的 Content
	Err    error
}

// RunSequential 按顺序串行执行多个步骤（A5.4）
//
// 终止条件：
//   - 任一 step 失败 → 立即返回累积结果（含 error）
//   - 全部成功 → 返回所有 StepResult
func (o *Orchestrator) RunSequential(ctx context.Context, steps []Step) []StepResult {
	out := make([]StepResult, 0, len(steps))
	for _, s := range steps {
		// 检查 ctx 取消
		if ctx.Err() != nil {
			out = append(out, StepResult{Step: s, Err: ctx.Err()})
			return out
		}
		res := o.runOne(ctx, s)
		out = append(out, res)
		if res.Err != nil {
			// 失败立即终止
			return out
		}
	}
	return out
}

// RunParallel 并发执行所有步骤（A5.4）
//
// 所有 step 都执行完才返回。错误聚合为首次出现的。
func (o *Orchestrator) RunParallel(ctx context.Context, steps []Step) []StepResult {
	results := make([]StepResult, len(steps))
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for i, s := range steps {
		wg.Add(1)
		go func(idx int, step Step) {
			defer wg.Done()
			if ctx.Err() != nil {
				results[idx] = StepResult{Step: step, Err: ctx.Err()}
				return
			}
			r := o.runOne(ctx, step)
			results[idx] = r
			if r.Err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = r.Err
				}
				errMu.Unlock()
			}
		}(i, s)
	}
	wg.Wait()
	return results
}

// runOne 执行单个 step
func (o *Orchestrator) runOne(ctx context.Context, s Step) StepResult {
	if o.dispatcher == nil {
		return StepResult{Step: s, Err: fmt.Errorf("orchestrator: nil dispatcher")}
	}
	a, err := o.dispatcher.DispatchByRole(s.AgentName)
	if err != nil {
		return StepResult{Step: s, Err: fmt.Errorf("dispatch %s: %w", s.AgentName, err)}
	}
	if a == nil {
		return StepResult{Step: s, Err: fmt.Errorf("dispatch %s: returned nil agent", s.AgentName)}
	}
	res, err := a.Run(ctx, s.UserInput)
	if err != nil {
		return StepResult{Step: s, Err: err}
	}
	return StepResult{Step: s, Output: res.Content}
}
