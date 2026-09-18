package agent

import (
	"strings"
	"testing"
)

// TestAgentSpec_Validate_OK 测试合法 spec 通过 Validate
func TestAgentSpec_Validate_OK(t *testing.T) {
	spec := &AgentSpec{
		Name:     "story-architect",
		Model:    "opus",
		MaxTurns: 30,
		Memory:   "project",
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate 应通过，实际报错：%v", err)
	}
	if spec.MaxTurns != 30 {
		t.Errorf("MaxTurns 应保留 =30，实际=%d", spec.MaxTurns)
	}
}

func TestAgentSpec_Validate_DefaultMaxTurns(t *testing.T) {
	spec := &AgentSpec{
		Name:  "story-architect",
		Model: "opus",
		// MaxTurns 留空 → 默认 30
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate 应通过，实际报错：%v", err)
	}
	if spec.MaxTurns != 30 {
		t.Errorf("默认 MaxTurns 应=30，实际=%d", spec.MaxTurns)
	}
}

func TestAgentSpec_Validate_DefaultMemory(t *testing.T) {
	spec := &AgentSpec{
		Name:  "story-architect",
		Model: "opus",
		// Memory 留空 → 默认 project
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate 应通过，实际报错：%v", err)
	}
	if spec.Memory != "project" {
		t.Errorf("默认 Memory 应=project，实际=%q", spec.Memory)
	}
}

func TestAgentSpec_Validate_MissingName(t *testing.T) {
	spec := &AgentSpec{Model: "opus"}
	if err := spec.Validate(); err == nil {
		t.Error("缺少 name 应报错")
	} else if !strings.Contains(err.Error(), "name") {
		t.Errorf("错误信息应含 name，实际=%q", err.Error())
	}
}

func TestAgentSpec_Validate_MissingModel(t *testing.T) {
	spec := &AgentSpec{Name: "story-architect"}
	if err := spec.Validate(); err == nil {
		t.Error("缺少 model 应报错")
	}
}

func TestAgentSpec_Validate_BadModel(t *testing.T) {
	spec := &AgentSpec{Name: "story-architect", Model: "gpt-5"}
	if err := spec.Validate(); err == nil {
		t.Error("非法 model 应报错")
	}
}

func TestAgentSpec_Validate_AllVendorModels(t *testing.T) {
	for _, m := range []string{"opus", "sonnet", "haiku"} {
		spec := &AgentSpec{Name: "test", Model: m}
		if err := spec.Validate(); err != nil {
			t.Errorf("vendor model %q 应通过，实际=%v", m, err)
		}
	}
}

func TestNewAgent(t *testing.T) {
	spec := &AgentSpec{Name: "story-architect", Model: "opus"}
	a := NewAgent(spec)
	if a == nil {
		t.Fatal("NewAgent 返回 nil")
	}
	if a.Spec != spec {
		t.Error("Agent.Spec 应指向传入的 spec")
	}
}

func TestAgent_Run_NotImplemented(t *testing.T) {
	// Sprint A1 阶段：Run 还未实现，应返回明确错误
	spec := &AgentSpec{Name: "story-architect", Model: "opus"}
	a := NewAgent(spec)
	_, err := a.Run(testCtx(), "test input")
	if err == nil {
		t.Error("Run 应返回 not implemented 错误")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("错误信息应含 'not implemented'，实际=%q", err.Error())
	}
}
