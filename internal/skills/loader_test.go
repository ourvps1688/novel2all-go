package skills

import (
	"strings"
	"testing"
)

func TestParseSkill_Valid(t *testing.T) {
	content := `---
name: story-test
description: "这是一个测试 skill"
---

# 测试标题

正文内容。
`
	s, err := parseSkill("story-test.md", content)
	if err != nil {
		t.Fatalf("parseSkill 失败：%v", err)
	}
	if s.Name != "story-test" {
		t.Errorf("Name=%q, want=story-test", s.Name)
	}
	if s.Description != "这是一个测试 skill" {
		t.Errorf("Description=%q", s.Description)
	}
	if !strings.Contains(s.Body, "测试标题") {
		t.Error("Body 应包含正文标题")
	}
	if !strings.Contains(s.Body, "正文内容") {
		t.Error("Body 应包含正文内容")
	}
}

func TestParseSkill_NoFrontmatter(t *testing.T) {
	_, err := parseSkill("bad.md", "# no frontmatter")
	if err == nil {
		t.Error("缺 frontmatter 应该报错")
	}
}

func TestParseSkill_NoClosing(t *testing.T) {
	_, err := parseSkill("bad.md", "---\nname: x\nno closing")
	if err == nil {
		t.Error("缺 closing --- 应该报错")
	}
}

func TestParseSkill_NoName(t *testing.T) {
	_, err := parseSkill("bad.md", "---\ndescription: x\n---\nbody")
	if err == nil {
		t.Error("缺 name 应该报错")
	}
}

func TestParseSkill_SingleQuote(t *testing.T) {
	content := `---
name: a
description: 'single quoted desc'
---
body
`
	_, err := parseSkill("a.md", content)
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
}

func TestNewLoader(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatalf("NewLoader 失败：%v", err)
	}
	count := l.Count()
	if count != 13 {
		t.Errorf("应加载 13 个 skill，实际=%d", count)
	}
	t.Logf("loaded %d skills", count)
}

func TestLoader_List(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}
	names := l.List()
	if len(names) != 13 {
		t.Errorf("List 应返回 13 个名字，实际=%d", len(names))
	}
	// 排序检查
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("List 未排序：%q >= %q", names[i-1], names[i])
			break
		}
	}
}

func TestLoader_Get(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}
	s, err := l.Get("story")
	if err != nil {
		t.Fatalf("Get story 失败：%v", err)
	}
	if s.Description == "" {
		t.Error("story 应有 description")
	}
	if !strings.Contains(s.Body, "路由") {
		t.Error("story body 应包含 '路由'")
	}
}

func TestLoader_Get_NotFound(t *testing.T) {
	l, _ := NewLoader()
	_, err := l.Get("non-existent-skill")
	if err == nil {
		t.Error("不存在的 skill 应该报错")
	}
}
