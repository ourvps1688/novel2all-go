// Package skills pipeline.go: stage runner 框架（P1-B 残余）
//
// 多个 skill 顺序执行，支持：
//   - Depends: stage 间的依赖（DAG 拓扑排序）
//   - Vars: 全局变量池（stage 输出自动存到 Vars[stage.Name]，可被后续 stage 引用）
//   - 模板替换: user input 支持 {{var}} 引用变量
//   - 流式 + 非流式执行
//
// 用法：
//
//	p := skills.NewPipeline(executor)
//	stages := []skills.Stage{
//	    {Name: "outline", Skill: "story-outline"},
//	    {Name: "draft", Skill: "story-write", Depends: []string{"outline"}},
//	    {Name: "review", Skill: "story-review", Depends: []string{"draft"}},
//	}
//	results, err := p.Run(ctx, stages)
//	// results["outline"].Output / results["draft"].Output / ...
package skills

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// Stage pipeline 中一个 stage
type Stage struct {
	// Name stage 唯一名（同 pipeline 内）
	Name string

	// Skill 要执行的 skill 名 (e.g. "story-outline")
	Skill string

	// Vars 输入变量（合并到全局 vars）
	Vars map[string]string

	// Depends 依赖的前置 stage names（满足后才能执行）
	Depends []string

	// Task 可选：llm.TaskType 覆盖默认 (e.g. "WRITING" / "EXTRACTION")
	Task string

	// Stream 是否流式（默认 false）
	Stream bool
}

// Result 单个 stage 的执行结果
type Result struct {
	Stage     string
	Output    string
	TokensIn  int
	TokensOut int
}

// ExecutorRunner Pipeline 需要的 executor 抽象（便于测试注入 mock）
//
// *Executor 自动实现此接口。
type ExecutorRunner interface {
	Execute(ctx context.Context, input ExecuteInput) (*ExecuteResult, error)
	ExecuteStream(ctx context.Context, input ExecuteInput, ch chan<- llm.Chunk) error
}

// Pipeline 多个 stages 的 DAG 执行器
type Pipeline struct {
	executor ExecutorRunner

	mu   sync.RWMutex
	vars map[string]string // 全局 vars（stage 间共享；stage 输出自动存到 vars[stage.Name]）
}

// NewPipeline 创建 pipeline
func NewPipeline(executor ExecutorRunner) *Pipeline {
	return &Pipeline{
		executor: executor,
		vars:     make(map[string]string),
	}
}

// SetVar 设置全局变量（在 Run 之前调用）
func (p *Pipeline) SetVar(key, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vars[key] = value
}

// GetVar 取全局变量（返回空字符串如果没设）
func (p *Pipeline) GetVar(key string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.vars[key]
}

// Vars 返回当前所有 vars 的快照
func (p *Pipeline) Vars() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]string, len(p.vars))
	for k, v := range p.vars {
		out[k] = v
	}
	return out
}

// Run 顺序执行所有 stages（按 Depends 拓扑排序）
//
// 返回 map[stage_name]Result
func (p *Pipeline) Run(ctx context.Context, stages []Stage) (map[string]Result, error) {
	if len(stages) == 0 {
		return map[string]Result{}, nil
	}

	// 1. 验证 + 建索引
	stageMap := make(map[string]Stage, len(stages))
	for _, s := range stages {
		if s.Name == "" {
			return nil, fmt.Errorf("stage with empty name")
		}
		if s.Skill == "" {
			return nil, fmt.Errorf("stage %q has empty Skill", s.Name)
		}
		if _, dup := stageMap[s.Name]; dup {
			return nil, fmt.Errorf("duplicate stage name %q", s.Name)
		}
		stageMap[s.Name] = s
	}
	for _, s := range stages {
		for _, dep := range s.Depends {
			if _, ok := stageMap[dep]; !ok {
				return nil, fmt.Errorf("stage %q depends on unknown stage %q", s.Name, dep)
			}
		}
	}

	// 2. 拓扑排序
	order, err := topoSort(stages)
	if err != nil {
		return nil, err
	}

	// 3. 执行
	results := make(map[string]Result, len(stages))
	for _, name := range order {
		s := stageMap[name]

		// 合并 stage.Vars 到全局 vars（覆盖）
		for k, v := range s.Vars {
			p.SetVar(k, v)
		}

		// 执行 stage
		result, err := p.runStage(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("stage %q: %w", s.Name, err)
		}

		results[s.Name] = result

		// 把 stage 输出存到 vars[stage.Name]（自动）
		p.SetVar(s.Name, result.Output)
	}

	return results, nil
}

// runStage 执行单个 stage（Executor + 模板替换）
func (p *Pipeline) runStage(ctx context.Context, s Stage) (Result, error) {
	// 用 stage.Name 作为 user input 模板（{{stage_name}} 引用）
	// 默认 user input 是 skill 名（也可以通过 Vars["__user_input__"] 覆盖）
	userInput := s.Name

	// 模板替换: 引用 vars 里的值
	userInput = renderTemplate(userInput, p.Vars())

	task := llm.TaskType(s.Task)
	if task == "" {
		task = llm.TaskUnknown
	}

	input := ExecuteInput{
		SkillName: s.Skill,
		UserInput: userInput,
		Task:      string(task),
	}

	if s.Stream {
		return p.runStageStream(ctx, input, s.Name)
	}

	res, err := p.executor.Execute(ctx, input)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Stage:     s.Name,
		Output:    res.Content,
		TokensIn:  res.TokensIn,
		TokensOut: res.TokensOut,
	}, nil
}

// runStageStream 流式执行（累加所有 chunks 到 output）
func (p *Pipeline) runStageStream(ctx context.Context, input ExecuteInput, stageName string) (Result, error) {
	ch := make(chan llm.Chunk, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.executor.ExecuteStream(ctx, input, ch)
		close(ch)
	}()

	var sb strings.Builder
	for chunk := range ch {
		sb.WriteString(chunk.Content)
	}
	if err := <-errCh; err != nil {
		return Result{}, err
	}

	return Result{
		Stage:  stageName,
		Output: sb.String(),
	}, nil
}

// renderTemplate 用 {{key}} 模板替换 vars[key]
//
// 不存在的 key 替换为空字符串
func renderTemplate(tmpl string, vars map[string]string) string {
	for {
		start := strings.Index(tmpl, "{{")
		if start < 0 {
			break
		}
		end := strings.Index(tmpl[start:], "}}")
		if end < 0 {
			break
		}
		key := strings.TrimSpace(tmpl[start+2 : start+end])
		val := vars[key] // 空字符串如果没设
		tmpl = tmpl[:start] + val + tmpl[start+end+2:]
	}
	return tmpl
}

// topoSort Kahn 算法拓扑排序
func topoSort(stages []Stage) ([]string, error) {
	inDeg := make(map[string]int, len(stages))
	adj := make(map[string][]string, len(stages))

	for _, s := range stages {
		if _, ok := inDeg[s.Name]; !ok {
			inDeg[s.Name] = 0
		}
		for _, dep := range s.Depends {
			inDeg[s.Name]++
			adj[dep] = append(adj[dep], s.Name)
		}
	}

	// 初始 queue: 所有入度 0 的 stage
	queue := make([]string, 0, len(stages))
	for name, d := range inDeg {
		if d == 0 {
			queue = append(queue, name)
		}
	}

	order := make([]string, 0, len(stages))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)
		// 对每个后继 stage -1 入度
		for _, next := range adj[cur] {
			inDeg[next]--
			if inDeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(order) != len(stages) {
		return nil, fmt.Errorf("circular dependency detected (resolved %d/%d stages)", len(order), len(stages))
	}
	return order, nil
}
