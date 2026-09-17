// verifier_test.go 测试 Verifier 工具函数.
package memory

import (
	"context"
	"strings"
	"testing"
)

func TestHasBlockingIssues(t *testing.T) {
	if HasBlockingIssues(nil) {
		t.Error("nil should return false")
	}
	if HasBlockingIssues([]ContinuityIssue{}) {
		t.Error("empty should return false")
	}

	warnings := []ContinuityIssue{
		{Severity: "warning", Category: "character", Description: "x"},
		{Severity: "info", Category: "setting", Description: "y"},
	}
	if HasBlockingIssues(warnings) {
		t.Error("no critical should return false")
	}

	warnings = append(warnings, ContinuityIssue{
		Severity: "critical", Category: "character", Description: "dead character alive",
	})
	withCritical := warnings
	if !HasBlockingIssues(withCritical) {
		t.Error("with critical should return true")
	}
}

func TestFilterIssues_NoFilter(t *testing.T) {
	issues := []ContinuityIssue{
		{Severity: "critical", Category: "character"},
		{Severity: "warning", Category: "foreshadowing"},
	}
	got := FilterIssues(issues, nil, nil)
	if len(got) != 2 {
		t.Errorf("no filter should return all, got %d", len(got))
	}
}

func TestFilterIssues_ByCategory(t *testing.T) {
	issues := []ContinuityIssue{
		{Severity: "critical", Category: "character"},
		{Severity: "warning", Category: "foreshadowing"},
		{Severity: "info", Category: "character"},
	}
	got := FilterIssues(issues, []string{"character"}, nil)
	if len(got) != 2 {
		t.Errorf("character filter should return 2, got %d", len(got))
	}
	for _, i := range got {
		if i.Category != "character" {
			t.Errorf("filtered out wrong category: %q", i.Category)
		}
	}
}

func TestFilterIssues_BySeverity(t *testing.T) {
	issues := []ContinuityIssue{
		{Severity: "critical", Category: "character"},
		{Severity: "warning", Category: "foreshadowing"},
		{Severity: "info", Category: "setting"},
	}
	got := FilterIssues(issues, nil, []string{"critical", "warning"})
	if len(got) != 2 {
		t.Errorf("severity filter should return 2, got %d", len(got))
	}
}

func TestFilterIssues_BothFilters(t *testing.T) {
	issues := []ContinuityIssue{
		{Severity: "critical", Category: "character"},
		{Severity: "critical", Category: "foreshadowing"},
		{Severity: "warning", Category: "character"},
	}
	got := FilterIssues(issues, []string{"character"}, []string{"critical"})
	if len(got) != 1 {
		t.Errorf("both filters should return 1, got %d", len(got))
	}
	if got[0].Severity != "critical" || got[0].Category != "character" {
		t.Errorf("filtered wrong: %+v", got[0])
	}
}

func TestFilterIssues_CaseInsensitive(t *testing.T) {
	issues := []ContinuityIssue{{Severity: "Critical", Category: "CHARACTER"}}
	got := FilterIssues(issues, []string{"character"}, []string{"critical"})
	if len(got) != 1 {
		t.Error("case-insensitive match should work")
	}
}

func TestGroupIssuesByCategory(t *testing.T) {
	issues := []ContinuityIssue{
		{Severity: "critical", Category: "character", Description: "a"},
		{Severity: "warning", Category: "character", Description: "b"},
		{Severity: "info", Category: "setting", Description: "c"},
	}
	groups := GroupIssuesByCategory(issues)
	if len(groups["character"]) != 2 {
		t.Errorf("character group = %d", len(groups["character"]))
	}
	if len(groups["setting"]) != 1 {
		t.Errorf("setting group = %d", len(groups["setting"]))
	}
}

func TestCountIssuesBySeverity(t *testing.T) {
	issues := []ContinuityIssue{
		{Severity: "critical"},
		{Severity: "critical"},
		{Severity: "warning"},
	}
	counts := CountIssuesBySeverity(issues)
	if counts["critical"] != 2 || counts["warning"] != 1 {
		t.Errorf("counts = %+v", counts)
	}
}

func TestVerifier_PreWriteCheck_NilRouter(t *testing.T) {
	v := NewVerifier(nil)
	issues, err := v.PreWriteCheck(context.Background(), "outline", newEmptyState("p"))
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if issues != nil {
		t.Errorf("nil router should return nil issues, got %v", issues)
	}
}

func TestVerifier_PreWriteCheck_EmptyOutline(t *testing.T) {
	v := NewVerifier(nil)
	issues, err := v.PreWriteCheck(context.Background(), "", newEmptyState("p"))
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if issues != nil {
		t.Errorf("empty outline should return nil issues, got %v", issues)
	}
}

func TestVerifier_PostWriteCheck_Stub(t *testing.T) {
	v := NewVerifier(nil)
	issues, err := v.PostWriteCheck(context.Background(), "content")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if issues != nil {
		t.Errorf("PostWriteCheck stub should return nil, got %v", issues)
	}
}

func TestStateToText(t *testing.T) {
	state := newEmptyState("test")
	state.Characters = map[string]CharacterState{
		"alice": {Name: "alice", Location: "town"},
	}
	state.Foreshadowing = map[string]ForeshadowingState{
		"fs1": {ID: "fs1", Status: "active", Description: "mystery"},
	}
	state.Timeline = []TimelineEvent{
		{Chapter: 1, Event: "story begins"},
	}
	text := stateToText(state)
	for _, want := range []string{"test", "alice", "town", "active", "mystery", "story begins"} {
		if !strings.Contains(text, want) {
			t.Errorf("state text missing %q: %s", want, text)
		}
	}
}

func TestStateToText_NilState(t *testing.T) {
	text := stateToText(nil)
	if !strings.Contains(text, "无状态") {
		t.Errorf("nil state should show placeholder, got: %s", text)
	}
}
