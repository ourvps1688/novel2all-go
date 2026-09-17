package main

import (
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/obs"
)

// llmHookAdapter 把 obs.Metrics + obs.TraceRecorder 桥接到 llm.MetricsHook 接口。
//
// 设计动机：llm 包不能直接 import obs 包（避免循环依赖 + 保持可选依赖）。
// main.go 负责拼装：创建 adapter 实例 → 注入到 llmRouter.SetMetricsHook。
type llmHookAdapter struct {
	m *obs.Metrics
	t *obs.TraceRecorder
}

// IncLLMCall 增加 LLM 调用计数。
func (a *llmHookAdapter) IncLLMCall(provider, task, status string) {
	a.m.IncLLMCall(provider, task, status)
}

// AddLLMTokens 增加 token 计数。
func (a *llmHookAdapter) AddLLMTokens(provider, kind string, n int64) {
	a.m.AddLLMTokens(provider, kind, n)
}

// RecordLLMTrace 记录 LLM 调用 trace。
func (a *llmHookAdapter) RecordLLMTrace(provider, task, status string, duration time.Duration, tokensIn, tokensOut int) {
	a.t.RecordLLM(provider, task, status, duration, tokensIn, tokensOut)
}

// 编译期断言：llmHookAdapter 实现 llm.MetricsHook 接口。
var _ llm.MetricsHook = (*llmHookAdapter)(nil)
