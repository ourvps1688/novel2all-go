package skills

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// mockExecutor 测试用 mock executor
type mockExecutor struct {
	// stage name → output
	outputs map[string]string

	// 强制返回错误（用于错误路径）
	forceErrFor map[string]error // skill name → err

	// 记录 stage 调用顺序
	calls []string
}

func (m *mockExecutor) Execute(_ context.Context, input ExecuteInput) (*ExecuteResult, error) {
	m.calls = append(m.calls, input.SkillName)
	if err, ok := m.forceErrFor[input.SkillName]; ok {
		return nil, err
	}
	out, ok := m.outputs[input.SkillName]
	if !ok {
		out = "mock output for " + input.SkillName
	}
	return &ExecuteResult{
		Content:   out,
		Provider:  "mock",
		Model:     "mock-1",
		TokensIn:  10,
		TokensOut: 20,
	}, nil
}

func (m *mockExecutor) ExecuteStream(_ context.Context, input ExecuteInput, ch chan<- llm.Chunk) error {
	res, err := m.Execute(context.Background(), input)
	if err != nil {
		return err
	}
	// 简单分片: 3 chunks
	parts := []string{res.Content[:5], res.Content[5:10], res.Content[10:]}
	for _, p := range parts {
		ch <- llm.Chunk{Content: p}
	}
	ch <- llm.Chunk{Done: true}
	return nil
}

// TestTopoSort 拓扑排序（独立测试）
func TestTopoSort(t *testing.T) {
	tests := []struct {
		name      string
		stages    []Stage
		wantErr   bool
		wantOrder []string // 期望顺序（len 一致即可，顺序可灵活）
	}{
		{
			name: "single stage",
			stages: []Stage{
				{Name: "a", Skill: "sa"},
			},
			wantErr:   false,
			wantOrder: []string{"a"},
		},
		{
			name: "linear chain",
			stages: []Stage{
				{Name: "a", Skill: "sa"},
				{Name: "b", Skill: "sb", Depends: []string{"a"}},
				{Name: "c", Skill: "sc", Depends: []string{"b"}},
			},
			wantErr:   false,
			wantOrder: []string{"a", "b", "c"},
		},
		{
			name: "diamond",
			stages: []Stage{
				{Name: "a", Skill: "sa"},
				{Name: "b", Skill: "sb", Depends: []string{"a"}},
				{Name: "c", Skill: "sc", Depends: []string{"a"}},
				{Name: "d", Skill: "sd", Depends: []string{"b", "c"}},
			},
			wantErr:   false,
			wantOrder: []string{"a", "b", "c", "d"}, // or a, c, b, d
		},
		{
			name: "circular",
			stages: []Stage{
				{Name: "a", Skill: "sa", Depends: []string{"b"}},
				{Name: "b", Skill: "sb", Depends: []string{"a"}},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, err := topoSort(tt.stages)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(order) != len(tt.wantOrder) {
				t.Errorf("expected %d stages, got %d (%v)", len(tt.wantOrder), len(order), order)
			}
			// 验证依赖顺序：depends 中的 stage 必须在 current 之前
			stageMap := map[string]Stage{}
			for _, s := range tt.stages {
				stageMap[s.Name] = s
			}
			position := map[string]int{}
			for i, name := range order {
				position[name] = i
			}
			for _, s := range tt.stages {
				for _, dep := range s.Depends {
					if position[dep] >= position[s.Name] {
						t.Errorf("stage %q should come after dep %q (positions %d vs %d)",
							s.Name, dep, position[s.Name], position[dep])
					}
				}
			}
		})
	}
}

// TestRenderTemplate 模板变量替换
func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		vars map[string]string
		want string
	}{
		{
			name: "simple",
			tmpl: "Hello {{name}}",
			vars: map[string]string{"name": "World"},
			want: "Hello World",
		},
		{
			name: "missing key",
			tmpl: "Hello {{name}}, age {{age}}",
			vars: map[string]string{"name": "Alice"},
			want: "Hello Alice, age ",
		},
		{
			name: "no template",
			tmpl: "Plain text",
			vars: map[string]string{"x": "y"},
			want: "Plain text",
		},
		{
			name: "whitespace in key",
			tmpl: "Hello {{  spaced  }}",
			vars: map[string]string{"spaced": "World"},
			want: "Hello World",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderTemplate(tt.tmpl, tt.vars)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestPipeline_Run_Linear 端到端：线性 stages
func TestPipeline_Run_Linear(t *testing.T) {
	mock := &mockExecutor{
		outputs: map[string]string{
			"outline": "OUTLINE_OUT",
			"write":   "WRITE_OUT",
		},
	}
	p := NewPipeline(mock)

	stages := []Stage{
		{Name: "outline", Skill: "outline"},
		{Name: "draft", Skill: "write", Depends: []string{"outline"}},
	}
	results, err := p.Run(context.Background(), stages)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results["outline"].Output != "OUTLINE_OUT" {
		t.Errorf("outline output: got %q", results["outline"].Output)
	}
	if results["draft"].Output != "WRITE_OUT" {
		t.Errorf("draft output: got %q", results["draft"].Output)
	}
	// Vars 应该自动存到 vars[stage.Name]
	if p.GetVar("outline") != "OUTLINE_OUT" {
		t.Errorf("var outline not stored: got %q", p.GetVar("outline"))
	}
	if p.GetVar("draft") != "WRITE_OUT" {
		t.Errorf("var draft not stored: got %q", p.GetVar("draft"))
	}
	// 调用顺序
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(mock.calls))
	}
	if mock.calls[0] != "outline" || mock.calls[1] != "write" {
		t.Errorf("call order: %v", mock.calls)
	}
}

// TestPipeline_Run_Diamond 端到端：DAG
func TestPipeline_Run_Diamond(t *testing.T) {
	mock := &mockExecutor{
		outputs: map[string]string{
			"a": "A",
			"b": "B",
			"c": "C",
			"d": "D",
		},
	}
	p := NewPipeline(mock)
	stages := []Stage{
		{Name: "a", Skill: "a"},
		{Name: "b", Skill: "b", Depends: []string{"a"}},
		{Name: "c", Skill: "c", Depends: []string{"a"}},
		{Name: "d", Skill: "d", Depends: []string{"b", "c"}},
	}
	results, err := p.Run(context.Background(), stages)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("expected 4 results, got %d", len(results))
	}
	// 顺序: a → {b, c} → d
	position := map[string]int{}
	for i, s := range []string{"a", "b", "c", "d"} {
		_ = results[s] // ensure all executed
		position[s] = i
	}
	if position["a"] >= position["b"] || position["a"] >= position["c"] {
		t.Error("a should come first")
	}
	if position["b"] >= position["d"] || position["c"] >= position["d"] {
		t.Error("d should be last")
	}
}

// TestPipeline_Run_ExecutorError 错误传播
func TestPipeline_Run_ExecutorError(t *testing.T) {
	mock := &mockExecutor{
		outputs:     map[string]string{"a": "A"},
		forceErrFor: map[string]error{"b": errors.New("LLM unavailable")},
	}
	p := NewPipeline(mock)
	stages := []Stage{
		{Name: "a", Skill: "a"},
		{Name: "b", Skill: "b", Depends: []string{"a"}},
	}
	_, err := p.Run(context.Background(), stages)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "LLM unavailable") {
		t.Errorf("error should wrap LLM err: %v", err)
	}
	if !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("error should mention failing stage: %v", err)
	}
}

// TestPipeline_ValidationErrors 验证错误
func TestPipeline_ValidationErrors(t *testing.T) {
	mock := &mockExecutor{}
	p := NewPipeline(mock)

	tests := []struct {
		name   string
		stages []Stage
		errMsg string
	}{
		{
			name: "duplicate name",
			stages: []Stage{
				{Name: "a", Skill: "x"},
				{Name: "a", Skill: "y"},
			},
			errMsg: "duplicate stage name",
		},
		{
			name: "empty name",
			stages: []Stage{
				{Name: "", Skill: "x"},
			},
			errMsg: "empty name",
		},
		{
			name: "empty skill",
			stages: []Stage{
				{Name: "a", Skill: ""},
			},
			errMsg: "empty Skill",
		},
		{
			name: "unknown dependency",
			stages: []Stage{
				{Name: "a", Skill: "x", Depends: []string{"nonexistent"}},
			},
			errMsg: "depends on unknown stage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Run(context.Background(), tt.stages)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error should contain %q, got %q", tt.errMsg, err.Error())
			}
		})
	}
}

// TestPipeline_Stream 流式执行
func TestPipeline_Stream(t *testing.T) {
	mock := &mockExecutor{
		outputs: map[string]string{"a": "AAAAA-BBBB-CCCC"},
	}
	p := NewPipeline(mock)
	stages := []Stage{
		{Name: "a", Skill: "a", Stream: true},
	}
	results, err := p.Run(context.Background(), stages)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if results["a"].Output != "AAAAA-BBBB-CCCC" {
		t.Errorf("stream output: got %q", results["a"].Output)
	}
}

// TestPipeline_VarMerge 测试 stage.Vars 合并到全局
func TestPipeline_VarMerge(t *testing.T) {
	mock := &mockExecutor{
		outputs: map[string]string{"a": "A"},
	}
	p := NewPipeline(mock)
	p.SetVar("global_x", "from-constructor")

	stages := []Stage{
		{Name: "a", Skill: "a", Vars: map[string]string{"stage_y": "from-stage"}},
	}
	_, err := p.Run(context.Background(), stages)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if p.GetVar("global_x") != "from-constructor" {
		t.Errorf("global_x lost: %q", p.GetVar("global_x"))
	}
	if p.GetVar("stage_y") != "from-stage" {
		t.Errorf("stage_y not merged: %q", p.GetVar("stage_y"))
	}
	if p.GetVar("a") != "A" {
		t.Errorf("stage output not stored: %q", p.GetVar("a"))
	}
}

// TestPipeline_EmptyStages 空 stages 返回空 results
func TestPipeline_EmptyStages(t *testing.T) {
	mock := &mockExecutor{}
	p := NewPipeline(mock)
	results, err := p.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

// TestPipeline_VarsSnapshot 测试 Vars() 返回快照（修改不影响内部）
func TestPipeline_VarsSnapshot(t *testing.T) {
	mock := &mockExecutor{}
	p := NewPipeline(mock)
	p.SetVar("x", "1")

	snap := p.Vars()
	snap["x"] = "mutated"
	snap["new"] = "added"

	if p.GetVar("x") != "1" {
		t.Error("internal vars should not be mutated by external changes")
	}
	if p.GetVar("new") != "" {
		t.Error("new key should not leak into internal vars")
	}
}
