// loader_test.go - internal/references 包的单元测试 (Sprint 35)
package references

import (
	"strings"
	"testing"
)

func TestNewLoader_LoadsEmbeddedFiles(t *testing.T) {
	l := NewLoader()
	if l == nil {
		t.Fatal("NewLoader returned nil")
	}
	names := l.AllNames()
	if len(names) == 0 {
		t.Fatal("expected >0 references loaded from embed.FS")
	}
	// 验证几个 starter 存在
	expected := []string{"style-anchor", "deslop-rules", "platform-style", "hook-techniques"}
	for _, e := range expected {
		found := false
		for _, n := range names {
			if n == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected reference %q in AllNames(), got %d names", e, len(names))
		}
	}
}

func TestLoadByName_ValidAndInvalid(t *testing.T) {
	l := NewLoader()

	got := l.LoadByName("style-anchor")
	if got == "" {
		t.Error("expected non-empty content for style-anchor")
	}
	if !strings.Contains(got, "文风") {
		t.Error("style-anchor content should mention 文风")
	}

	got = l.LoadByName("nonexistent-reference")
	if got != "" {
		t.Errorf("expected empty string for unknown name, got %d chars", len(got))
	}
}

func TestLoadForSkill_DefaultsAlwaysPresent(t *testing.T) {
	l := NewLoader()

	// 任意 skill 都应包含 default refs
	defaults := []string{"style-anchor", "deslop-rules", "platform-style"}

	tests := []struct {
		skill string
	}{
		{"story-long-write"},
		{"story-short-write"},
		{"story-review"},
		{"story-deslop"},
		{""}, // 空 skill 也应返回 default
	}

	for _, tt := range tests {
		t.Run(tt.skill, func(t *testing.T) {
			refs := l.LoadForSkill(tt.skill)
			for _, want := range defaults {
				found := false
				for _, r := range refs {
					if r == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("skill=%q: default %q should be in refs", tt.skill, want)
				}
			}
		})
	}
}

func TestLoadForSkill_SkillSpecificRefs(t *testing.T) {
	l := NewLoader()

	tests := []struct {
		skill string
		want  []string
	}{
		{"story-long-write", []string{"outline-structure", "character-design", "foreshadowing"}},
		{"story-short-write", []string{"hook-techniques", "twist-design", "short-pacing"}},
		{"story-review", []string{"review-perspectives", "foreshadowing", "consistency-check"}},
		{"story-short-scan", []string{"short-platform-style"}},
		{"story-cover", []string{"cover-styles", "copywriting-templates"}},
	}

	for _, tt := range tests {
		t.Run(tt.skill, func(t *testing.T) {
			refs := l.LoadForSkill(tt.skill)
			for _, w := range tt.want {
				found := false
				for _, r := range refs {
					if r == w {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("skill=%q: expected %q in refs, got %v", tt.skill, w, refs)
				}
			}
		})
	}
}

func TestLoadForSkill_UnknownSkillStillReturnsDefaults(t *testing.T) {
	l := NewLoader()
	refs := l.LoadForSkill("totally-unknown-skill-name")
	if len(refs) == 0 {
		t.Error("unknown skill should still return default refs")
	}
	// 应至少含 style-anchor (default)
	hasDefault := false
	for _, r := range refs {
		if r == "style-anchor" {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		t.Error("unknown skill should still include style-anchor default")
	}
}

func TestLoadForSkill_Deduplication(t *testing.T) {
	l := NewLoader()
	// story-deslop 含 deslop-rules 和 ai-tells (但 deslop-rules 也是 default)
	refs := l.LoadForSkill("story-deslop")
	// 验证无重复
	seen := make(map[string]int)
	for _, r := range refs {
		seen[r]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("reference %q appears %d times in LoadForSkill result", name, count)
		}
	}
}

func TestLoadForSkill_Sorted(t *testing.T) {
	l := NewLoader()
	refs := l.LoadForSkill("story-long-write")
	for i := 1; i < len(refs); i++ {
		if refs[i-1] > refs[i] {
			t.Errorf("refs not sorted at index %d: %q > %q", i, refs[i-1], refs[i])
		}
	}
}

func TestStats(t *testing.T) {
	l := NewLoader()
	loaded, skills, defaultCount := l.Stats()
	if loaded < 20 {
		t.Errorf("expected >=20 references loaded, got %d", loaded)
	}
	if skills < 10 {
		t.Errorf("expected >=10 skills registered, got %d", skills)
	}
	if defaultCount < 1 {
		t.Errorf("expected >=1 default ref, got %d", defaultCount)
	}
}

func TestListSkills(t *testing.T) {
	l := NewLoader()
	skills := l.ListSkills()
	if len(skills) == 0 {
		t.Fatal("ListSkills should return non-empty")
	}
	// 验证 sorted
	for i := 1; i < len(skills); i++ {
		if skills[i-1] > skills[i] {
			t.Errorf("skills not sorted: %q > %q", skills[i-1], skills[i])
		}
	}
	// 验证包含关键 skill
	for _, want := range []string{"story-long-write", "story-short-write", "story-review"} {
		found := false
		for _, s := range skills {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in ListSkills, got %v", want, skills)
		}
	}
}

// TestSprint35_DuckType_ApiInterface 验证 Loader duck-type 满足 api.ReferencesLoader 接口.
//
// 这是避免循环 import 的核心契约: api.ReferencesLoader 期望 LoadForSkill + LoadByName,
// Loader 提供这两个方法.
func TestSprint35_DuckType_ApiInterface(t *testing.T) {
	var _ interface {
		LoadForSkill(string) []string
		LoadByName(string) string
	} = (*Loader)(nil)
}

// TestLoadForSkill_ReturnsCopy 验证返回的 slice 是 copy (调用方修改不影响内部).
func TestLoadForSkill_ReturnsCopy(t *testing.T) {
	l := NewLoader()
	refs := l.LoadForSkill("story-long-write")
	original := len(refs)
	refs[0] = "MUTATED"
	refs2 := l.LoadForSkill("story-long-write")
	if refs2[0] == "MUTATED" {
		t.Error("LoadForSkill should return a copy, mutation should not affect subsequent calls")
	}
	if len(refs2) != original {
		t.Errorf("subsequent call returned different length: %d vs %d", len(refs2), original)
	}
}
