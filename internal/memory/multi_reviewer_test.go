// multi_reviewer_test.go 测试 MultiAgentReviewer.
package memory

import (
	"context"
	"strings"
	"testing"
)

func TestReviewerRoles(t *testing.T) {
	if len(ReviewerRoles) != 4 {
		t.Errorf("ReviewerRoles count = %d, want 4", len(ReviewerRoles))
	}
	want := []string{"story_outliner", "chapter_writer", "consistency_checker", "story_reviewer"}
	for i, r := range want {
		if ReviewerRoles[i] != r {
			t.Errorf("ReviewerRoles[%d] = %q, want %q", i, ReviewerRoles[i], r)
		}
	}
}

func TestMultiAgentReviewer_Review_NilRouter(t *testing.T) {
	r := NewMultiAgentReviewer(nil)
	// V0: nil router → stub 模式, 不调 LLM
	report, err := r.Review(context.Background(), 1, "content", newEmptyState("test"))
	if err != nil {
		t.Errorf("nil router should still return stub report, got err: %v", err)
	}
	if report == nil {
		t.Error("report should not be nil")
	}
}

func TestMultiAgentReviewer_Review_All4Reviewers(t *testing.T) {
	r := NewMultiAgentReviewer(nil) // V0 stub: 不调 LLM
	report, err := r.Review(context.Background(), 1, "test content", newEmptyState("test"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Chapter != 1 {
		t.Errorf("chapter = %d", report.Chapter)
	}
	if len(report.Scores) != 4 {
		t.Errorf("scores count = %d, want 4 (one per reviewer)", len(report.Scores))
	}
	if report.Summary == "" {
		t.Error("summary should not be empty")
	}
}

func TestMultiAgentReviewer_ConsistencyCheck_DeadCharacter(t *testing.T) {
	r := NewMultiAgentReviewer(nil)
	state := newEmptyState("test")
	state.Characters["alice"] = CharacterState{Name: "alice", Motivation: "dead"}

	issues := r.checkConsistency(context.Background(), "consistency_checker", "", state)
	if len(issues) == 0 {
		t.Error("dead character should produce issue")
	}
	if issues[0].Reviewer != "consistency_checker" {
		t.Errorf("reviewer = %q", issues[0].Reviewer)
	}
	if issues[0].Severity != "warning" {
		t.Errorf("severity = %q, want warning", issues[0].Severity)
	}
}

func TestMultiAgentReviewer_ConsistencyCheck_NoDeadChar(t *testing.T) {
	r := NewMultiAgentReviewer(nil)
	state := newEmptyState("test")
	state.Characters["alice"] = CharacterState{Name: "alice", Motivation: "alive"}

	issues := r.checkConsistency(context.Background(), "consistency_checker", "", state)
	if len(issues) != 0 {
		t.Errorf("should have 0 issues, got %d", len(issues))
	}
}

func TestSummarizeReport(t *testing.T) {
	report := &ReviewReport{}
	s := summarizeReport(report)
	if s != "no issues found" {
		t.Errorf("empty report summary = %q", s)
	}
}

func TestMultiAgentReviewer_ReviewIssuesHaveReviewer(t *testing.T) {
	r := NewMultiAgentReviewer(nil)
	state := newEmptyState("test")
	state.Characters["x"] = CharacterState{Name: "x", Motivation: "dead"}
	report, _ := r.Review(context.Background(), 1, "", state)
	for _, issue := range report.Issues {
		if issue.Reviewer == "" {
			t.Error("issue missing reviewer tag")
		}
	}
}

func TestStateToText_Exported(t *testing.T) {
	state := newEmptyState("test")
	state.Characters = map[string]CharacterState{
		"alice": {Name: "alice", Location: "town"},
	}
	text := StateToText(state)
	if text == "" {
		t.Error("StateToText should not return empty for non-nil state")
	}
	if !strings.Contains(text, "alice") {
		t.Errorf("missing character in StateToText: %s", text)
	}
}

func TestStateToText_NilState_Exported(t *testing.T) {
	text := StateToText(nil)
	if !strings.Contains(text, "无状态") {
		t.Errorf("nil state should return placeholder, got: %s", text)
	}
}
