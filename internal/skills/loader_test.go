package skills

import (
	"strings"
	"testing"
)

func TestParseSkillMD_Valid(t *testing.T) {
	content := `---
name: story-test
description: "这是一个测试 skill"
---

# 测试标题

正文内容。
`
	name, desc, body, err := parseSkillMD(content)
	if err != nil {
		t.Fatalf("parseSkillMD 失败：%v", err)
	}
	if name != "story-test" {
		t.Errorf("name=%q, want=story-test", name)
	}
	if desc != "这是一个测试 skill" {
		t.Errorf("desc=%q", desc)
	}
	if !strings.Contains(body, "测试标题") {
		t.Error("body 应包含正文标题")
	}
	if !strings.Contains(body, "正文内容") {
		t.Error("body 应包含正文内容")
	}
}

func TestParseSkillMD_NoFrontmatter(t *testing.T) {
	_, _, _, err := parseSkillMD("# no frontmatter")
	if err == nil {
		t.Error("缺 frontmatter 应该报错")
	}
}

func TestParseSkillMD_NoClosing(t *testing.T) {
	_, _, _, err := parseSkillMD("---\nname: x\nno closing")
	if err == nil {
		t.Error("缺 closing --- 应该报错")
	}
}

func TestParseSkillMD_NoName(t *testing.T) {
	_, _, _, err := parseSkillMD("---\ndescription: x\n---\nbody")
	if err == nil {
		t.Error("缺 name 应该报错")
	}
}

func TestParseSkillMD_SingleQuote(t *testing.T) {
	content := `---
name: a
description: 'single quoted desc'
---
body
`
	_, desc, _, err := parseSkillMD(content)
	if err != nil {
		t.Fatalf("parseSkillMD: %v", err)
	}
	if desc != "single quoted desc" {
		t.Errorf("single quote desc=%q", desc)
	}
}

func TestNewLoader_13Skills(t *testing.T) {
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

func TestLoader_ListSorted(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}
	names := l.List()
	if len(names) != 13 {
		t.Errorf("List 应返回 13 个名字，实际=%d", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("List 未排序：%q >= %q", names[i-1], names[i])
			break
		}
	}
}

func TestLoader_GetValid(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"story", "story-setup", "story-deslop"} {
		s, err := l.Get(name)
		if err != nil {
			t.Errorf("Get %q: %v", name, err)
			continue
		}
		if s.Description == "" {
			t.Errorf("%q 应有 description", name)
		}
	}
}

func TestLoader_Get_NotFound(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.Get("non-existent-skill")
	if err == nil {
		t.Error("不存在的 skill 应该报错")
	}
}

// TestLoader_References 验证 references 列表正确
func TestLoader_References(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		skill        string
		wantMinRefs  int
		wantRefMatch string // 期望至少 1 个 reference 路径含此子串
	}{
		{"story-deslop", 5, ".md"},
		{"story-setup", 1, ".md"},
		{"story-long-write", 48, ".md"},
		{"story-short-write", 23, ".md"},
		{"story-review", 11, ".md"},
	}
	for _, tt := range tests {
		s, err := l.Get(tt.skill)
		if err != nil {
			t.Errorf("Get %q: %v", tt.skill, err)
			continue
		}
		if len(s.References) < tt.wantMinRefs {
			t.Errorf("%q 应有至少 %d 个 reference，实际=%d", tt.skill, tt.wantMinRefs, len(s.References))
		}
		// 验证至少 1 个含 .md
		foundMd := false
		for _, r := range s.References {
			if strings.Contains(r, tt.wantRefMatch) {
				foundMd = true
				break
			}
		}
		if !foundMd {
			t.Errorf("%q 的 references 应至少 1 个含 %q，实际=%v", tt.skill, tt.wantRefMatch, s.References)
		}
	}
}

// TestLoader_LoadReference 验证按需加载 reference + L1 cache
func TestLoader_LoadReference(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}

	// 加载 story-deslop 的一个 reference
	s, err := l.Get("story-deslop")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.References) == 0 {
		t.Skip("story-deslop 无 references，跳过")
	}

	refPath := s.References[0]
	content1, err := l.LoadReference("story-deslop", refPath)
	if err != nil {
		t.Fatalf("LoadReference: %v", err)
	}
	if content1 == "" {
		t.Error("reference content 应非空")
	}

	// 第二次调用应走 cache（返回相同内容）
	content2, err := l.LoadReference("story-deslop", refPath)
	if err != nil {
		t.Fatalf("LoadReference 2nd: %v", err)
	}
	if content1 != content2 {
		t.Error("L1 cache 应返回相同内容")
	}
}

// TestLoader_LoadReference_NotInList 加载不在 references 列表中的文件应报错
func TestLoader_LoadReference_NotInList(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.LoadReference("story-deslop", "../etc/passwd")
	if err == nil {
		t.Error("不在 references 列表中的文件应被拒绝（防 ../ 逃避）")
	}
}

func TestLoader_LoadReference_NestedPath(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}

	// story-setup 有 agent-references 子目录
	s, err := l.Get("story-setup")
	if err != nil {
		t.Fatal(err)
	}
	// 找 nested reference
	var nestedRef string
	for _, r := range s.References {
		if strings.Contains(r, "/") {
			nestedRef = r
			break
		}
	}
	if nestedRef == "" {
		t.Skip("story-setup 无 nested reference，跳过")
	}
	content, err := l.LoadReference("story-setup", nestedRef)
	if err != nil {
		t.Fatalf("LoadReference nested %q: %v", nestedRef, err)
	}
	if content == "" {
		t.Error("nested reference 应非空")
	}
}

func TestLoader_LoadAllReferences(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatal(err)
	}
	all, err := l.LoadAllReferences("story-deslop")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := l.Get("story-deslop")
	if len(all) != len(s.References) {
		t.Errorf("LoadAllReferences 应返回 %d 个，实际=%d", len(s.References), len(all))
	}
}

func TestExtractSkillsFromBody(t *testing.T) {
	tests := []struct {
		body string
		want []string
	}{
		{"# title\nskills: [story-deslop, story-setup]\n", []string{"story-deslop", "story-setup"}},
		{"# title\nskills: [story-cover]\n# end\n", []string{"story-cover"}},
		{"# no skills here", nil},
		{"skills:[\n  story-a,\n  story-b,\n]", []string{"story-a", "story-b"}},
	}
	for _, tt := range tests {
		got := extractSkillsFromBody(tt.body)
		if len(got) != len(tt.want) {
			t.Errorf("body=%q: got %v, want %v", tt.body, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("body=%q: got[%d]=%q, want %q", tt.body, i, got[i], tt.want[i])
			}
		}
	}
}
