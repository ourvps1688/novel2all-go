package skills

import (
	"strings"
	"testing"
)

// TestSkillsE2E_All13Vendored 端到端验证 13 个 SKILL.md 都已通过 embed.FS 加载 (Sprint A6.3)
func TestSkillsE2E_All13Vendored(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	all := l.List()
	if len(all) != 13 {
		t.Errorf("want 13 skills, got %d (list=%v)", len(all), all)
	}

	for _, skillName := range all {
		s, err := l.Get(skillName)
		if err != nil {
			t.Errorf("Get %s: %v", skillName, err)
			continue
		}
		if s.Name == "" {
			t.Errorf("skill %s name 为空", skillName)
		}
		if s.Description == "" {
			t.Errorf("skill %s description 为空", skillName)
		}
		if s.Body == "" {
			t.Errorf("skill %s body 为空", skillName)
		}
	}
}

// TestSkillsE2E_ReferencesAccessible 验证 references 可通过 LoadReference 访问 (Sprint A6.3)
func TestSkillsE2E_ReferencesAccessible(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	tests := []struct {
		skill string
		min   int // min references
	}{
		{"story-long-write", 1},  // 48 references
		{"story-short-write", 1}, // 23 references
		{"story-review", 1}, // 11 references
		{"story-setup", 1}, // 1 reference
		{"story-deslop", 1}, // 5 references
	}

	for _, tt := range tests {
		s, err := l.Get(tt.skill)
		if err != nil {
			t.Errorf("Get %s: %v", tt.skill, err)
			continue
		}
		if len(s.References) < tt.min {
			t.Errorf("%s 应有至少 %d references，实际=%d", tt.skill, tt.min, len(s.References))
			continue
		}
		// Try loading first reference
		content, err := l.LoadReference(tt.skill, s.References[0])
		if err != nil {
			t.Errorf("LoadReference %s/%s: %v", tt.skill, s.References[0], err)
			continue
		}
		if len(content) < 50 {
			t.Errorf("%s/%s content 过短（%d 字节）", tt.skill, s.References[0], len(content))
		}
	}
}

// TestSkillsE2E_NestedReferences 验证 nested subdirs 也可访问 (genre-prose-cards 等)
func TestSkillsE2E_NestedReferences(t *testing.T) {
	l, err := NewLoader()
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	all := l.List()
	var foundSkill, foundNested string
	for _, skillName := range all {
		s, err := l.Get(skillName)
		if err != nil {
			continue
		}
		for _, r := range s.References {
			if strings.Contains(r, "/") {
				foundSkill = skillName
				foundNested = r
				break
			}
		}
		if foundNested != "" {
			break
		}
	}
	if foundNested == "" {
		t.Skip("无任何 nested reference")
	}
	content, err := l.LoadReference(foundSkill, foundNested)
	if err != nil {
		t.Fatalf("LoadReference %s/%s: %v", foundSkill, foundNested, err)
	}
	if content == "" {
		t.Errorf("nested %s/%s content 为空", foundSkill, foundNested)
	}
}