package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/agent/tools"
	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// TestDispatcher_DispatchByStage 测试 Stage → Agent 映射 (A5.5)
func TestDispatcher_DispatchByStage(t *testing.T) {
	agents := map[string]*Agent{
		"story-architect":     {},
		"narrative-writer":    {},
		"consistency-checker": {},
	}
	d := NewDispatcher(agents)

	tests := []struct {
		stage    string
		wantRole string
	}{
		{"outline", "story-architect"},
		{"chapter_write", "narrative-writer"},
		{"consistency_check", "consistency-checker"},
	}
	for _, tt := range tests {
		a, err := d.DispatchByStage(tt.stage)
		if err != nil {
			t.Errorf("DispatchByStage(%q): %v", tt.stage, err)
			continue
		}
		if a == nil {
			t.Errorf("DispatchByStage(%q): returned nil", tt.stage)
		}
	}
}

// TestDispatcher_DispatchByRole 测试 vendor 原名映射 (A5.5)
func TestDispatcher_DispatchByRole(t *testing.T) {
	agents := map[string]*Agent{
		"story-architect":  {Spec: &AgentSpec{Name: "story-architect"}},
		"narrative-writer": {Spec: &AgentSpec{Name: "narrative-writer"}},
	}
	d := NewDispatcher(agents)

	a, err := d.DispatchByRole("story-architect")
	if err != nil || a == nil || a.Spec.Name != "story-architect" {
		t.Errorf("DispatchByRole(story-architect): %v %+v", err, a)
	}

	// not found
	_, err = d.DispatchByRole("nonexistent")
	if err == nil {
		t.Error("未注册 agent 应报错")
	}
}

// TestDispatcher_AvailableRoles 测试列出所有可用 role
func TestDispatcher_AvailableRoles(t *testing.T) {
	agents := map[string]*Agent{
		"story-architect":    {},
		"narrative-writer":   {},
		"character-designer": {},
	}
	d := NewDispatcher(agents)

	roles := d.AvailableRoles()
	if len(roles) != 3 {
		t.Errorf("应有 3 个 role，实际=%d", len(roles))
	}
	// 应按字典序
	for i := 1; i < len(roles); i++ {
		if roles[i-1] >= roles[i] {
			t.Errorf("未排序：%v", roles)
			break
		}
	}
}

// TestOrchestrator_RunSequential 测试串行 (A5.4)
func TestOrchestrator_RunSequential(t *testing.T) {
	// 准备 agents（每个都带 mock LLM + tool adapter）
	agents := map[string]*Agent{
		"story-architect":  makeMockAgent("arch-output"),
		"narrative-writer": makeMockAgent("writer-output"),
	}
	d := NewDispatcher(agents)
	o := NewOrchestrator(d)

	steps := []Step{
		{AgentName: "story-architect", UserInput: "outline this"},
		{AgentName: "narrative-writer", UserInput: "write chapter 1"},
	}
	results := o.RunSequential(context.Background(), steps)
	if len(results) != 2 {
		t.Fatalf("应有 %d 结果，实际=%d", len(steps), len(results))
	}
	for i, r := range results {
		if r.Err != nil {
			t.Errorf("step %d err: %v", i, r.Err)
			continue
		}
		expected := []string{"arch-output", "writer-output"}[i]
		if r.Output != expected {
			t.Errorf("step %d output=%q, want %q", i, r.Output, expected)
		}
	}
}

// TestOrchestrator_RunParallel 测试并发 (A5.4)
func TestOrchestrator_RunParallel(t *testing.T) {
	agents := map[string]*Agent{
		"story-architect":     makeMockAgent("a-output"),
		"narrative-writer":    makeMockAgent("b-output"),
		"consistency-checker": makeMockAgent("c-output"),
	}
	d := NewDispatcher(agents)
	o := NewOrchestrator(d)

	steps := []Step{
		{AgentName: "story-architect", UserInput: "x"},
		{AgentName: "narrative-writer", UserInput: "y"},
		{AgentName: "consistency-checker", UserInput: "z"},
	}
	results := o.RunParallel(context.Background(), steps)
	if len(results) != 3 {
		t.Fatalf("应有 %d 结果，实际=%d", len(steps), len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("step err: %v", r.Err)
		}
	}
}

// TestOrchestrator_RunSequential_StopOnError 测试遇错终止 (A5.4)
func TestOrchestrator_RunSequential_StopOnError(t *testing.T) {
	// 不存在的 agent → 第一步就失败
	agents := map[string]*Agent{
		"story-architect": makeMockAgent("a"),
	}
	d := NewDispatcher(agents)
	o := NewOrchestrator(d)

	steps := []Step{
		{AgentName: "nonexistent", UserInput: "x"},     // 失败
		{AgentName: "story-architect", UserInput: "y"}, // 不应执行
	}
	results := o.RunSequential(context.Background(), steps)
	if len(results) != 1 {
		t.Errorf("遇错应只返 1 个结果，实际=%d", len(results))
	}
	if results[0].Err == nil {
		t.Error("第一个 step 应报错")
	}
}

// TestToolAdapter_DisallowedTools 测试 A5.16 防御层
func TestToolAdapter_DisallowedTools(t *testing.T) {
	reg := tools.NewRegistry()
	readTool := &mockTool{name: "Read", description: "r", schema: []byte(`{}`)}
	reg.Register(readTool)

	a := NewToolAdapter(reg, "/tmp")
	a.SetDisallowedTools([]string{"Write", "Edit"})

	// 调 Read 应成功（不在 disallowed）
	res := a.Dispatch("Read", []byte(`{}`))
	if res.IsError {
		t.Errorf("Read 应可调：%v", res.Content)
	}

	// 调 Write 应被拒（disallowed）
	res = a.Dispatch("Write", []byte(`{}`))
	if !res.IsError {
		t.Error("Write 应被 disallowed 拒绝")
	}
	if !strings.Contains(res.Content, "disallowed") {
		t.Errorf("错误信息应含 'disallowed'，实际=%q", res.Content)
	}

	// 不存在的 tool 应报错
	res = a.Dispatch("Bash", []byte(`{}`))
	if !res.IsError {
		t.Error("未注册 tool 应报错")
	}
}

// makeMockAgent 构造带 mock LLM 的 agent（输出指定 content）
func makeMockAgent(content string) *Agent {
	mockLLM := &mockLLMProvider{
		Responses: []*llm.ChatWithToolsResponse{
			{Content: content, TokensIn: 5, TokensOut: 10},
		},
	}
	toolsReg := tools.NewRegistry()
	// 给 agent 注册一个 Read tool（orchestrator 不会真用，但 RunAgent 需要它）
	_ = toolsReg.Register(&mockTool{name: "Read", description: "r", schema: []byte(`{}`)})

	return &Agent{
		Spec: &AgentSpec{
			Name:         "mock",
			Model:        "haiku",
			MaxTurns:     1,
			SystemPrompt: "mock prompt",
		},
		Router:          mockLLM,
		Tools:           NewToolAdapter(toolsReg, "/tmp"),
		Mapping:         DefaultModelMapping(),
		Translator:      NewPathTranslator(),
		ProjectRoot:     "/tmp",
		DisallowedTools: []string{},
	}
}
