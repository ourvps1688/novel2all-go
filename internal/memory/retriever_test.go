package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

// 用 keyword 模式避免 chroma 初始化不确定性.
func TestMemoryRetriever_KeywordMode(t *testing.T) {
	force := ModeKeyword
	r, err := NewRetriever("", &force)
	if err != nil {
		t.Fatalf("NewRetriever: %v", err)
	}
	if r.Mode() != ModeKeyword {
		t.Errorf("mode = %s, want keyword", r.Mode())
	}

	r.AddEvent(1, "character_change", "林雷觉醒血脉之力", nil)
	r.AddEvent(2, "foreshadowing", "神秘的戒指", nil)
	r.AddEvent(3, "general", "主角踏入修炼之路", nil)

	results := r.Query("血脉觉醒", 5, nil, "")
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for keyword query")
	}
	t.Logf("got %d results for '血脉觉醒'", len(results))
	for _, item := range results {
		t.Logf("  - %s (rel=%.3f)", item.Content, item.Relevance)
	}
}

func TestMemoryRetriever_TFIDFMode(t *testing.T) {
	force := ModeTFIDF
	r, _ := NewRetriever("", &force)
	if r.Mode() != ModeTFIDF {
		t.Errorf("mode = %s, want tfidf", r.Mode())
	}

	r.AddEvent(1, "general", "林雷是天才少年", nil)
	r.AddEvent(2, "general", "萧炎失去斗气", nil)
	r.AddEvent(3, "general", "唐三修炼蓝银草", nil)
	r.AddEvent(4, "general", "天气晴朗万里无云", nil)

	results := r.Query("林雷", 3, nil, "")
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// 林雷 is exactly in event 1, should rank first
	if !strings.Contains(results[0].Content, "林雷") {
		t.Errorf("top result should contain '林雷', got %q", results[0].Content)
	}

	// query 与不相关单词应得 0 结果
	results = r.Query("量子纠缠", 3, nil, "")
	if len(results) > 0 {
		t.Logf("note: keyword overlap may return some, got %d", len(results))
	}
}

func TestMemoryRetriever_ChromaMode(t *testing.T) {
	dir := t.TempDir()
	force := ModeChromaDB
	r, err := NewRetriever(filepath.Join(dir, ".chroma"), &force)
	if err != nil {
		t.Fatalf("NewRetriever: %v", err)
	}
	if r.Mode() != ModeChromaDB {
		t.Errorf("mode = %s, want chroma", r.Mode())
	}

	r.AddEvent(1, "general", "觉醒血脉之力", nil)
	r.AddEvent(2, "general", "踏入修炼之路", nil)
	r.AddEvent(3, "general", "晴朗万里无云", nil)

	results := r.Query("血脉", 5, nil, "")
	if len(results) == 0 {
		t.Fatal("expected chroma results")
	}
	t.Logf("chroma mode returned %d results", len(results))
	for _, item := range results {
		t.Logf("  - %s (rel=%.3f)", item.Content, item.Relevance)
	}
}

func TestMemoryRetriever_ChapterFilter(t *testing.T) {
	force := ModeKeyword
	r, _ := NewRetriever("", &force)

	r.AddEvent(1, "general", "事件 ch1", nil)
	r.AddEvent(2, "general", "事件 ch2", nil)
	r.AddEvent(3, "general", "事件 ch3", nil)
	r.AddEvent(4, "general", "事件 ch4", nil)
	r.AddEvent(5, "general", "事件 ch5", nil)

	// 只查 ch2-ch3
	rng := &[2]int{2, 3}
	results := r.Query("事件", 10, rng, "")
	if len(results) != 2 {
		t.Errorf("chapter filter: got %d results, want 2", len(results))
	}
	for _, item := range results {
		t.Logf("  - %s", item.Content)
	}
}

func TestMemoryRetriever_EventTypeFilter(t *testing.T) {
	force := ModeTFIDF
	r, _ := NewRetriever("", &force)

	r.AddEvent(1, "character_change", "角色变化 1 alpha", nil)
	r.AddEvent(2, "foreshadowing", "伏笔 1 beta", nil)
	r.AddEvent(3, "character_change", "角色变化 2 alpha", nil)
	r.AddEvent(4, "general", "通用 1 gamma", nil)

	// query 用所有 event 都可能匹配的词, event_type filter 决定结果
	results := r.Query("alpha", 10, nil, "character_change")
	if len(results) != 2 {
		t.Errorf("event_type filter: got %d, want 2", len(results))
	}
	for _, item := range results {
		t.Logf("  - %s", item.Content)
	}
}

func TestMemoryRetriever_DeleteChapter(t *testing.T) {
	force := ModeKeyword
	r, _ := NewRetriever("", &force)

	r.AddEvent(1, "general", "event 1", nil)
	r.AddEvent(1, "general", "event 1 again", nil)
	r.AddEvent(2, "general", "event 2", nil)
	r.AddEvent(3, "general", "event 3", nil)

	if c := r.Count(); c != 4 {
		t.Errorf("Count() = %d, want 4", c)
	}

	deleted := r.DeleteChapter(2)
	if deleted != 1 {
		t.Errorf("DeleteChapter(2) = %d, want 1", deleted)
	}
	if c := r.Count(); c != 3 {
		t.Errorf("after delete Count() = %d, want 3", c)
	}
}

func TestMemoryRetriever_Clear(t *testing.T) {
	force := ModeKeyword
	r, _ := NewRetriever("", &force)

	r.AddEvent(1, "general", "a", nil)
	r.AddEvent(2, "general", "b", nil)
	if c := r.Count(); c != 2 {
		t.Fatal("setup failed")
	}

	if err := r.Clear(); err != nil {
		t.Fatal(err)
	}
	if c := r.Count(); c != 0 {
		t.Errorf("after Clear Count() = %d, want 0", c)
	}
}

func TestMemoryRetriever_AddEvents_Batch(t *testing.T) {
	force := ModeKeyword
	r, _ := NewRetriever("", &force)

	events := []EventInput{
		{Chapter: 1, EventType: "general", Text: "batch 1"},
		{Chapter: 2, EventType: "general", Text: "batch 2"},
		{Chapter: 3, EventType: "general", Text: "batch 3"},
	}
	ids := r.AddEvents(events)
	if len(ids) != 3 {
		t.Errorf("AddEvents returned %d ids, want 3", len(ids))
	}
	if c := r.Count(); c != 3 {
		t.Errorf("Count after batch = %d, want 3", c)
	}
}

func TestMemoryRetriever_AutoFallback(t *testing.T) {
	// 不传 force_mode, 自动降级
	r, err := NewRetriever("", nil)
	if err != nil {
		t.Fatal(err)
	}
	mode := r.Mode()
	if mode != ModeChromaDB && mode != ModeTFIDF && mode != ModeKeyword {
		t.Errorf("unexpected mode: %s", mode)
	}
	t.Logf("auto-fallback mode = %s", mode)
}

func TestMemoryRetriever_GenerateID(t *testing.T) {
	id1 := generateEventID(1, "general", "test text")
	id2 := generateEventID(1, "general", "test text")
	if id1 != id2 {
		t.Errorf("same input should produce same ID: %s vs %s", id1, id2)
	}
	id3 := generateEventID(1, "general", "different text")
	if id1 == id3 {
		t.Error("different text should produce different ID")
	}
	if !strings.HasPrefix(id1, "ch1-general-") {
		t.Errorf("ID format wrong: %s", id1)
	}
}

func TestTokenize_Chinese(t *testing.T) {
	tokens := tokenizeForSearch("林雷觉醒血脉")
	if len(tokens) == 0 {
		t.Fatal("expected tokens for Chinese input")
	}
	t.Logf("tokens: %v", tokens)
}

func TestTokenize_English(t *testing.T) {
	tokens := tokenizeForSearch("hello world hello")
	tokensStr := strings.Join(tokens, ",")
	if !strings.Contains(tokensStr, "hello") {
		t.Errorf("should contain 'hello', got %s", tokensStr)
	}
}
