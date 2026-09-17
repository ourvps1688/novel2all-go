// verifier_test.go 测试 Verifier 工具函数.
package memory

import "testing"

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

	withCritical := append(warnings, ContinuityIssue{
		Severity: "critical", Category: "character", Description: "dead character alive",
	})
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
