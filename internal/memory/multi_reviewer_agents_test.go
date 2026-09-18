package memory

import (
	"context"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/agent"
)

// TestMultiAgentReviewer_ReviewWithAgentsStub 测试 A5.7 stub 实现
func TestMultiAgentReviewer_ReviewWithAgentsStub(t *testing.T) {
	r := NewMultiAgentReviewer(nil) // router=nil → stub 模式
	report, err := r.ReviewWithAgents(
		context.Background(),
		1, "test chapter content", &TrackingState{},
		nil, // agents=nil stub
	)
	if err != nil {
		t.Fatalf("ReviewWithAgents (stub): %v", err)
	}
	if report == nil {
		t.Fatal("Report 不应为 nil")
	}
	if report.Chapter != 1 {
		t.Errorf("Chapter=%d, want 1", report.Chapter)
	}
}

// TestReviewerAgentSet_Stub 测试 ReviewerAgentSet 类型存在（Sprint A5.7 接入点）
func TestReviewerAgentSet_Stub(t *testing.T) {
	set := &agent.ReviewerAgentSet{
		Reviewers: map[string]*agent.ReviewerAgent{
			"story_outliner":      {Role: "story_outliner"},
			"chapter_writer":      {Role: "chapter_writer"},
			"consistency_checker": {Role: "consistency_checker"},
			"story_reviewer":      {Role: "story_reviewer"},
		},
	}
	if len(set.Reviewers) != 4 {
		t.Errorf("应有 4 个 reviewer，实际=%d", len(set.Reviewers))
	}
}
