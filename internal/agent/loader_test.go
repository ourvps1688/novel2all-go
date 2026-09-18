package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testCtx 返回测试用 context（永不取消）
func testCtx() context.Context {
	return context.Background()
}

// TestParseFrontmatter_Simple 测试简单 frontmatter 解析
func TestParseFrontmatter_Simple(t *testing.T) {
	content := `---
name: story-architect
description: 故事架构与世界观创作专家
tools: [Read, Glob, Grep, Write, Edit]
model: opus
maxTurns: 30
memory: project
---

# Story Architect -- 故事架构师

你是故事架构师，负责网文创作的宏观层面。
`
	fm, body, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("ParseFrontmatter 失败：%v", err)
	}
	if fm.Name != "story-architect" {
		t.Errorf("Name=%q, want %q", fm.Name, "story-architect")
	}
	if fm.Model != "opus" {
		t.Errorf("Model=%q, want %q", fm.Model, "opus")
	}
	if fm.MaxTurns != 30 {
		t.Errorf("MaxTurns=%d, want 30", fm.MaxTurns)
	}
	if fm.Memory != "project" {
		t.Errorf("Memory=%q, want %q", fm.Memory, "project")
	}
	if len(fm.Tools) != 5 {
		t.Errorf("Tools 长度=%d, want 5, 实际=%v", len(fm.Tools), fm.Tools)
	}
	if !strings.Contains(body, "Story Architect") {
		t.Error("body 应包含 'Story Architect'")
	}
}

// TestParseFrontmatter_MultilineDescription 测试多行 description
func TestParseFrontmatter_MultilineDescription(t *testing.T) {
	content := `---
name: test-role
description: |
  第一行描述
  第二行描述
  第三行
model: sonnet
---

# Body
`
	fm, _, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("ParseFrontmatter 失败：%v", err)
	}
	if !strings.Contains(fm.Description, "第一行") {
		t.Errorf("Description 应含 '第一行'，实际=%q", fm.Description)
	}
	if !strings.Contains(fm.Description, "第三行") {
		t.Errorf("Description 应含 '第三行'，实际=%q", fm.Description)
	}
}

// TestParseFrontmatter_MultiLineToolsList 测试多行 tools 列表
func TestParseFrontmatter_MultiLineToolsList(t *testing.T) {
	content := `---
name: test-role
tools:
  - Read
  - Glob
  - Grep
model: haiku
---
body
`
	fm, _, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("ParseFrontmatter 失败：%v", err)
	}
	if len(fm.Tools) != 3 {
		t.Errorf("Tools 长度=%d, want 3, 实际=%v", len(fm.Tools), fm.Tools)
	}
	expected := []string{"Read", "Glob", "Grep"}
	for i, want := range expected {
		if i < len(fm.Tools) && fm.Tools[i] != want {
			t.Errorf("Tools[%d]=%q, want %q", i, fm.Tools[i], want)
		}
	}
}

// TestParseFrontmatter_NoClosing 测试缺少 ---
func TestParseFrontmatter_NoClosing(t *testing.T) {
	content := `---
name: test
no closing
`
	_, _, err := ParseFrontmatter(content)
	if err == nil {
		t.Error("缺少 --- 应报错")
	}
}

// TestParseFrontmatter_NoStart 测试没有起始 ---
func TestParseFrontmatter_NoStart(t *testing.T) {
	content := `name: test
---
body
`
	_, _, err := ParseFrontmatter(content)
	if err == nil {
		t.Error("缺少起始 --- 应报错")
	}
}

// TestParseFrontmatter_Comments 测试注释行
func TestParseFrontmatter_Comments(t *testing.T) {
	content := `---
# 这是注释
name: test-role
# 另一个注释
model: opus
---
body
`
	fm, _, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("ParseFrontmatter 失败：%v", err)
	}
	if fm.Name != "test-role" {
		t.Errorf("Name=%q, want %q", fm.Name, "test-role")
	}
}

// TestLoadAgentSpec_FromFile 测试从文件加载
func TestLoadAgentSpec_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-role.md")
	content := `---
name: test-role
description: test description
tools: [Read]
model: opus
maxTurns: 30
---

# Test Role Body

This is the body.
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	spec, err := LoadAgentSpec(path)
	if err != nil {
		t.Fatalf("LoadAgentSpec: %v", err)
	}
	if spec.Name != "test-role" {
		t.Errorf("Name=%q", spec.Name)
	}
	if !strings.Contains(spec.SystemPrompt, "Test Role Body") {
		t.Error("SystemPrompt 应含 'Test Role Body'")
	}
	if spec.SourcePath != path {
		t.Errorf("SourcePath=%q, want %q", spec.SourcePath, path)
	}
}

// TestLoadAgentSpec_All7VendorRoles 模拟加载 vendor 7 个 role (Sprint A1.7)
func TestLoadAgentSpec_All7VendorRoles(t *testing.T) {
	dir := t.TempDir()
	vendorRoles := []struct {
		name     string
		model    string
		maxTurns int
		memory   string
	}{
		{"story-architect", "opus", 30, "project"},
		{"narrative-writer", "sonnet", 40, "project"},
		{"character-designer", "sonnet", 30, "project"},
		{"consistency-checker", "haiku", 15, "project"},
		{"chapter-extractor", "haiku", 15, "project"},
		{"story-explorer", "haiku", 20, "project"},
		{"story-researcher", "sonnet", 30, "project"},
	}

	for _, r := range vendorRoles {
		content := "---\n" +
			"name: " + r.name + "\n" +
			"description: test\n" +
			"model: " + r.model + "\n" +
			"maxTurns: " + itoa(r.maxTurns) + "\n" +
			"memory: " + r.memory + "\n" +
			"---\n\n# " + r.name + " body\n"
		path := filepath.Join(dir, r.name+".md")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", r.name, err)
		}
	}

	// 批量加载
	specs, err := LoadAgentSpecFromDir(dir)
	if err != nil {
		t.Fatalf("LoadAgentSpecFromDir: %v", err)
	}
	if len(specs) != 7 {
		t.Errorf("应加载 7 个 role，实际=%d", len(specs))
	}

	for _, r := range vendorRoles {
		spec, ok := specs[r.name]
		if !ok {
			t.Errorf("role %q 未加载", r.name)
			continue
		}
		if spec.Model != r.model {
			t.Errorf("role %q: Model=%q, want %q", r.name, spec.Model, r.model)
		}
		if spec.MaxTurns != r.maxTurns {
			t.Errorf("role %q: MaxTurns=%d, want %d", r.name, spec.MaxTurns, r.maxTurns)
		}
	}
}

// TestRegistry_RegisterAndGet 测试 Registry 注册和查询
func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	spec := &AgentSpec{Name: "story-architect", Model: "opus"}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if !reg.Has("story-architect") {
		t.Error("Has 应返回 true")
	}

	a, err := reg.Get("story-architect")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.Spec.Name != "story-architect" {
		t.Errorf("Agent.Spec.Name=%q", a.Spec.Name)
	}
}

// TestRegistry_DuplicateRegister 测试重复注册报错
func TestRegistry_DuplicateRegister(t *testing.T) {
	reg := NewRegistry()
	spec := &AgentSpec{Name: "story-architect", Model: "opus"}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := reg.Register(spec); err == nil {
		t.Error("重复注册应报错")
	}
}

// TestRegistry_GetNotFound 测试查不存在的 agent
func TestRegistry_GetNotFound(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Error("Get 不存在的 agent 应报错")
	}
}

// TestRegistry_List 测试列出所有 agent
func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()
	for _, name := range []string{"story-architect", "narrative-writer", "consistency-checker"} {
		_ = reg.Register(&AgentSpec{Name: name, Model: "opus"})
	}
	list := reg.List()
	if len(list) != 3 {
		t.Errorf("List 长度=%d, want 3, 实际=%v", len(list), list)
	}
}

// TestRegistry_LoadFromDir 测试从目录批量加载
func TestRegistry_LoadFromDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"role-a", "role-b"} {
		content := "---\nname: " + name + "\nmodel: opus\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	reg := NewRegistry()
	if err := reg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if reg.Count() != 2 {
		t.Errorf("Count=%d, want 2", reg.Count())
	}
}

// itoa 简易 int → string，避免引入 strconv 依赖
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
