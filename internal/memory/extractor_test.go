// extractor_test.go 测试 Extractor.ApplyToState + helpers.
package memory

import (
	"testing"
)

func TestExtractor_ApplyToState_Characters(t *testing.T) {
	state := newEmptyState("test")
	state.Characters["林雷"] = CharacterState{
		Name: "林雷", Location: "苍茫镇", LastUpdatedChapter: 1,
	}

	ext := &Extractor{}
	extracted := &ExtractedChapterInfo{
		Chapter: 5,
		CharacterUpdates: []CharacterUpdate{
			{Name: "林雷", Location: "玉兰城", EmotionalState: "决绝"},
			{Name: "迪莉娅", Location: "玉兰城", KnowledgeAdded: []string{"血脉秘密"}},
		},
	}

	state = ext.ApplyToState(state, extracted)

	if state.Characters["林雷"].Location != "玉兰城" {
		t.Errorf("林雷.Location = %q, want 玉兰城", state.Characters["林雷"].Location)
	}
	if state.Characters["林雷"].EmotionalState != "决绝" {
		t.Errorf("林雷.EmotionalState = %q", state.Characters["林雷"].EmotionalState)
	}
	if state.Characters["迪莉娅"].Knowledge[0] != "血脉秘密" {
		t.Errorf("迪莉娅.Knowledge[0] = %q", state.Characters["迪莉娅"].Knowledge[0])
	}
	if state.Characters["林雷"].LastUpdatedChapter != 5 {
		t.Errorf("LastUpdatedChapter = %d, want 5", state.Characters["林雷"].LastUpdatedChapter)
	}
}

func TestExtractor_ApplyToState_Foreshadowing(t *testing.T) {
	state := newEmptyState("test")
	ext := &Extractor{}
	extracted := &ExtractedChapterInfo{
		Chapter: 3,
		ForeshadowingPlanted: []ForeshadowingUpdate{
			{ID: "fs:bloodline", Description: "血脉之谜", Status: "active"},
		},
		ForeshadowingChanged: []ForeshadowingUpdate{
			{ID: "fs:old_secret", Status: "revealed", Notes: "已揭示"},
		},
	}
	state = ext.ApplyToState(state, extracted)

	fs, ok := state.Foreshadowing["fs:bloodline"]
	if !ok {
		t.Fatal("fs:bloodline not planted")
	}
	if fs.PlantedChapter != 3 || fs.Status != "active" {
		t.Errorf("fs:bloodline = %+v", fs)
	}
	if state.Foreshadowing["fs:old_secret"].Notes != "已揭示" {
		t.Error("foreshadowing_changed not applied")
	}
}

func TestExtractor_ApplyToState_TimelineAndSummary(t *testing.T) {
	state := newEmptyState("test")
	ext := &Extractor{}
	extracted := &ExtractedChapterInfo{
		Chapter: 2,
		TimelineEvents: []TimelineEventUpdate{
			{InWorldTime: "三年夏", Event: "觉醒仪式", RelatedChars: []string{"林雷"}},
		},
		Summary: "本章主角觉醒血脉",
	}
	state = ext.ApplyToState(state, extracted)

	if len(state.Timeline) != 1 || state.Timeline[0].Event != "觉醒仪式" {
		t.Errorf("timeline = %+v", state.Timeline)
	}
	if state.RecentChapterSummaries[2] != "本章主角觉醒血脉" {
		t.Error("summary not applied")
	}
	if state.LastUpdatedChapter != 2 {
		t.Error("LastUpdatedChapter not updated")
	}
}

func TestExtractor_ApplyToState_GraphMerge(t *testing.T) {
	state := newEmptyState("test")
	state.Graph = &GraphData{
		Nodes: []GraphNode{{ID: "char:alice", Name: "Alice"}},
		Edges: []GraphEdge{},
	}

	ext := &Extractor{}
	extracted := &ExtractedChapterInfo{
		Chapter: 1,
		Graph: ExtractedGraphData{
			Nodes: []ExtractedGraphNode{
				{ID: "char:bob", Type: NodeCharacter, Name: "Bob"},
				{ID: "char:alice", Type: NodeCharacter, Name: "Alice Duplicate"},
			},
			Edges: []ExtractedGraphEdge{
				{FromID: "char:alice", ToID: "char:bob", Type: EdgeRelatedTo},
			},
		},
	}
	state = ext.ApplyToState(state, extracted)

	// alice 应该被 dedup (id 已存在), bob 新增
	if len(state.Graph.Nodes) != 2 {
		t.Errorf("nodes count = %d, want 2 (alice + bob)", len(state.Graph.Nodes))
	}
	if len(state.Graph.Edges) != 1 {
		t.Errorf("edges count = %d, want 1", len(state.Graph.Edges))
	}
}

func TestPickStr(t *testing.T) {
	if pickStr("a", "b") != "a" {
		t.Error("first non-empty should win")
	}
	if pickStr("", "b") != "b" {
		t.Error("empty should fall through")
	}
	if pickStr("", "") != "" {
		t.Error("both empty")
	}
}

func TestMergeStrSlice(t *testing.T) {
	got := mergeStrSlice([]string{"a", "b"}, []string{"b", "c"})
	if len(got) != 3 {
		t.Errorf("merged = %v, want 3 unique", got)
	}
}

func TestStateToPromptDict(t *testing.T) {
	state := newEmptyState("test")
	state.Characters["x"] = CharacterState{Name: "x"}
	d := stateToPromptDict(state)
	if d["project_name"] != "test" {
		t.Error("project_name missing")
	}
	if _, ok := d["characters"].(map[string]CharacterState); !ok {
		t.Error("characters wrong type")
	}
}

func TestIndexOf(t *testing.T) {
	if indexOf("hello world", "world") != 6 {
		t.Error("world at 6")
	}
	if indexOf("hello", "xyz") != -1 {
		t.Error("xyz not found")
	}
	if indexOf("", "x") != -1 {
		t.Error("empty src")
	}
}

func TestReplacePlaceholder(t *testing.T) {
	got := replacePlaceholder("Hello {name}! {name}!", "{name}", "World")
	if got != "Hello World! World!" {
		t.Errorf("got %q", got)
	}
}
