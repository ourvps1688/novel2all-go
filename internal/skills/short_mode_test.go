package skills

import (
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/roles"
)

func TestShortModeStages_Count(t *testing.T) {
	stages := ShortModeStages("test idea")
	if len(stages) != 8 {
		t.Errorf("expected 8 stages, got %d", len(stages))
	}
}

func TestShortModeStages_Names(t *testing.T) {
	stages := ShortModeStages("test")
	names := make([]string, len(stages))
	for i, s := range stages {
		names[i] = s.Name
	}
	expected := []string{
		"outline", "character_setup", "chapter_write", "consistency_check",
		"chapter_review", "deslop", "polish", "final",
	}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("stage[%d] = %q, expected %q", i, names[i], name)
		}
	}
}

func TestShortModeStages_TopoOrder(t *testing.T) {
	stages := ShortModeStages("test")
	// outline 是第一个, final 是最后一个, final depends on polish
	if stages[0].Name != "outline" {
		t.Errorf("first stage should be outline, got %q", stages[0].Name)
	}
	if stages[len(stages)-1].Name != "final" {
		t.Errorf("last stage should be final, got %q", stages[len(stages)-1].Name)
	}
}

func TestShortModeStages_Dependencies(t *testing.T) {
	stages := ShortModeStages("test")
	// 验证依赖关系
	for _, s := range stages {
		if s.Name == "character_setup" {
			if len(s.Depends) != 1 || s.Depends[0] != "outline" {
				t.Errorf("character_setup should depend only on outline, got %v", s.Depends)
			}
		}
		if s.Name == "final" {
			if len(s.Depends) != 1 || s.Depends[0] != "polish" {
				t.Errorf("final should depend only on polish, got %v", s.Depends)
			}
		}
	}
}

func TestShortModeStagesForRole(t *testing.T) {
	tests := []struct {
		stage string
		role  roles.Role
	}{
		{"outline", roles.RoleStoryOutliner},
		{"unknown", ""},
	}
	for _, tt := range tests {
		got, ok := ShortModeStagesForRole(tt.stage)
		if ok && got != tt.role {
			t.Errorf("stage %q: expected %q, got %q", tt.stage, tt.role, got)
		}
		if !ok && tt.role != "" {
			t.Errorf("stage %q: expected ok", tt.stage)
		}
	}
}

func TestShortModeStages_PromptTemplate(t *testing.T) {
	stages := ShortModeStages("A boy and girl meet in library")
	// Find chapter_write stage
	for _, s := range stages {
		if s.Name == "chapter_write" {
			ui, ok := s.Vars["__user_input__"]
			if !ok {
				t.Error("__user_input__ not set")
			}
			// Should reference character_setup output
			if !strings.Contains(ui, "{{character_setup}}") {
				t.Errorf("chapter_write prompt should reference character_setup, got: %q", ui)
			}
		}
	}
}
