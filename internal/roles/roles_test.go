package roles

import "testing"

func TestAll(t *testing.T) {
	all := All()
	if len(all) != 5 {
		t.Errorf("expected 5 roles, got %d", len(all))
	}
}

func TestGet(t *testing.T) {
	r, err := Get(RoleChapterWriter)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if r.ID != RoleChapterWriter {
		t.Errorf("expected ID %q, got %q", RoleChapterWriter, r.ID)
	}
	if r.Name == "" {
		t.Error("Name should not be empty")
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := Get("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

func TestIsValid(t *testing.T) {
	if !IsValid(RoleStoryOutliner) {
		t.Error("RoleStoryOutliner should be valid")
	}
	if IsValid("nonexistent") {
		t.Error("nonexistent should not be valid")
	}
}

func TestAgents(t *testing.T) {
	agents := Agents()
	if len(agents) != 5 {
		t.Errorf("expected 5 agents, got %d", len(agents))
	}
	if agents[RoleStoryOutliner] != "outliner" {
		t.Errorf("outliner agent mismatch")
	}
}

func TestStageToRole(t *testing.T) {
	r, ok := StageToRole("outline")
	if !ok {
		t.Error("expected ok for stage outline")
	}
	if r.ID != RoleStoryOutliner {
		t.Errorf("expected story_outliner, got %s", r.ID)
	}

	_, ok = StageToRole("nonexistent")
	if ok {
		t.Error("expected not ok for unknown stage")
	}
}
