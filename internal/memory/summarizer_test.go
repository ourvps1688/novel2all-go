// summarizer_test.go 测试 ChapterSummarizer.
package memory

import (
	"strings"
	"testing"
)

func TestTierOf(t *testing.T) {
	tests := []struct {
		ch   int
		want int
	}{
		{1, 1}, {5, 1}, {10, 1},
		{11, 2}, {15, 2}, {25, 2}, {30, 2},
		{31, 3}, {50, 3}, {100, 3},
	}
	for _, tt := range tests {
		if got := TierOf(tt.ch); got != tt.want {
			t.Errorf("TierOf(%d) = %d, want %d", tt.ch, got, tt.want)
		}
	}
}

func TestBucketOf(t *testing.T) {
	tests := []struct {
		ch                 int
		wantStart, wantEnd int
	}{
		{1, 1, 1},
		{10, 10, 10},
		{11, 11, 15},
		{15, 11, 15},
		{16, 16, 20},
		{30, 26, 30},
		{31, 31, 40},
		{50, 41, 50},
	}
	for _, tt := range tests {
		gotS, gotE := BucketOf(tt.ch)
		if gotS != tt.wantStart || gotE != tt.wantEnd {
			t.Errorf("BucketOf(%d) = (%d, %d), want (%d, %d)", tt.ch, gotS, gotE, tt.wantStart, tt.wantEnd)
		}
	}
}

func TestSummarizer_SummarizeChapter_Short(t *testing.T) {
	s := NewChapterSummarizer()
	short := "这是一段短文本。"
	if got := s.SummarizeChapter(short); got != short {
		t.Errorf("short content should be returned as-is, got %q", got)
	}
}

func TestSummarizer_SummarizeChapter_Long(t *testing.T) {
	s := NewChapterSummarizer()
	long := "第一句。林雷走在路上。第二句。他来到苍茫镇。第三句。他看到了玉兰城。最后一句。他决定了。"
	got := s.SummarizeChapter(long)
	// 应该包含前 3 句和末 1 句
	if !strings.Contains(got, "林雷走在路上") {
		t.Error("missing first sentence")
	}
	if !strings.Contains(got, "决定了") {
		t.Error("missing last sentence")
	}
}

func TestSummarizer_ApplyToState_Tier1(t *testing.T) {
	state := newEmptyState("test")
	s := NewChapterSummarizer()
	content := "第一句。第二句。第三句。最后一句。第五句。"
	state = s.ApplyToState(state, 5, content)
	if _, ok := state.RecentChapterSummaries[5]; !ok {
		t.Errorf("chapter 5 should have summary, got keys: %v", state.RecentChapterSummaries)
	}
}

func TestSummarizer_ApplyToState_Tier2(t *testing.T) {
	state := newEmptyState("test")
	s := NewChapterSummarizer()
	content := "第一句。第二句。第三句。最后一句。"
	// ch 11 + ch 12 应该都合并到 bucket [11, 15]
	s.ApplyToState(state, 11, content)
	s.ApplyToState(state, 12, content)
	summary, ok := state.RecentChapterSummaries[11]
	if !ok {
		t.Fatal("bucket 11 should exist")
	}
	if !strings.Contains(summary, "Ch 11-15:") {
		t.Errorf("bucket summary should have prefix, got %q", summary)
	}
}

func TestSummarizer_CountTokens(t *testing.T) {
	s := NewChapterSummarizer()
	if got := s.CountTokens(""); got != 0 {
		t.Errorf("empty should be 0, got %d", got)
	}
	if got := s.CountTokens("hello world"); got < 1 {
		t.Errorf("non-empty should be > 0, got %d", got)
	}
}

func TestSplitSentences(t *testing.T) {
	got := splitSentences("第一句。第二句！第三句？English. Final!")
	if len(got) != 5 {
		t.Errorf("split into %d, want 5: %v", len(got), got)
	}
}

func TestSummarizer_CompressRange(t *testing.T) {
	s := NewChapterSummarizer()
	state := newEmptyState("p")
	state.RecentChapterSummaries = map[int]string{
		1: "ch1 summary",
		2: "ch2 summary",
		3: "ch3 summary",
	}
	got := s.CompressRange(state, 1, 3)
	for _, want := range []string{"ch1", "ch2", "ch3"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in compressed: %s", want, got)
		}
	}
}

func TestSummarizer_CompressRange_Empty(t *testing.T) {
	s := NewChapterSummarizer()
	state := newEmptyState("p")
	got := s.CompressRange(state, 1, 5)
	if got != "" {
		t.Errorf("empty range should return empty string, got %q", got)
	}
}

func TestSummarizer_CompressRange_NilState(t *testing.T) {
	s := NewChapterSummarizer()
	got := s.CompressRange(nil, 1, 5)
	if got != "" {
		t.Errorf("nil state should return empty, got %q", got)
	}
}

func TestSummarizer_MergeBuckets(t *testing.T) {
	s := NewChapterSummarizer()
	buckets := [3][]string{
		{"recent1", "recent2"},
		{"mid1"},
		{"far1", "far2", "far3"},
	}
	got := s.MergeBuckets(buckets)
	for _, want := range []string{"TIER1", "TIER2", "TIER3", "recent1", "mid1", "far1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in merged: %s", want, got)
		}
	}
}

func TestSummarizer_MergeBuckets_AllEmpty(t *testing.T) {
	s := NewChapterSummarizer()
	got := s.MergeBuckets([3][]string{})
	if got != "" {
		t.Errorf("all-empty should return empty, got %q", got)
	}
}
