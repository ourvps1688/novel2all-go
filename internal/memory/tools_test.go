package memory

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMemoryTools_4ToolsRegistered 验证 MemoryTools 返回 4 个 tool.
func TestMemoryTools_4ToolsRegistered(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, MemoryConfig{
		CoreTokenBudget: 3000, CharacterTokenBudget: 4000, RecentChapterCount: 5, EventTopK: 8,
	})
	tools := Tools(mm)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	expectedNames := []string{"graph_query", "foreshadow_query", "timeline_query", "character_query"}
	for i, name := range expectedNames {
		if tools[i].Name != name {
			t.Errorf("tool[%d].Name = %q, want %q", i, tools[i].Name, name)
		}
		if tools[i].Handler == nil {
			t.Errorf("tool[%d] (%s) has nil handler", i, name)
		}
		if len(tools[i].Parameters) == 0 {
			t.Errorf("tool[%d] (%s) has empty Parameters schema", i, name)
		}
	}
}

// TestCharacterQueryTool 测 character_query 找存在/不存在的角色.
func TestCharacterQueryTool(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, MemoryConfig{
		CoreTokenBudget: 3000, CharacterTokenBudget: 4000, RecentChapterCount: 5, EventTopK: 8,
	})
	if _, err := mm.Tracker().Init("test"); err != nil {
		t.Fatal(err)
	}

	// 加 1 个角色
	state, _ := mm.LoadState(mm.projectName())
	state.Characters["林雷"] = CharacterState{
		Name:           "林雷",
		Location:       "东林镇",
		EmotionalState: "平静",
		Motivation:     "修炼",
		Knowledge:      []string{"初级剑术"},
	}
	if err := mm.Tracker().Write(state); err != nil {
		t.Fatal(err)
	}

	// 找 tool
	tools := Tools(mm)
	for _, t2 := range tools {
		_ = t2
	}

	// 调 handler
	tool := tools[3] // character_query 在第 4 位
	ctx := context.Background()

	// 存在
	args, _ := json.Marshal(map[string]string{"name": "林雷"})
	res, err := tool.Handler(ctx, args)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	m, _ := res.(map[string]any)
	if m["found"] != true {
		t.Errorf("expected found=true, got %v", m["found"])
	}
	if m["location"] != "东林镇" {
		t.Errorf("expected location=东林镇, got %v", m["location"])
	}

	// 不存在
	args, _ = json.Marshal(map[string]string{"name": "霍格"})
	res, err = tool.Handler(ctx, args)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	m, _ = res.(map[string]any)
	if m["found"] != false {
		t.Errorf("expected found=false, got %v", m["found"])
	}
}

// TestForeshadowQueryTool 测 foreshadow_query 过滤 + 全量.
func TestForeshadowQueryTool(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, MemoryConfig{
		CoreTokenBudget: 3000, CharacterTokenBudget: 4000, RecentChapterCount: 5, EventTopK: 8,
	})
	if _, err := mm.Tracker().Init("test"); err != nil {
		t.Fatal(err)
	}

	state, _ := mm.LoadState(mm.projectName())
	state.Foreshadowing["fs1"] = ForeshadowingState{
		ID: "fs1", Description: "伏笔1", PlantedChapter: 3, Status: "active",
	}
	state.Foreshadowing["fs2"] = ForeshadowingState{
		ID: "fs2", Description: "伏笔2", PlantedChapter: 5, Status: "active",
	}
	if err := mm.Tracker().Write(state); err != nil {
		t.Fatal(err)
	}

	tools := Tools(mm)
	tool := tools[1] // foreshadow_query
	ctx := context.Background()

	// 不传 chapter → 全 active
	args, _ := json.Marshal(map[string]any{})
	res, err := tool.Handler(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m["count"] != 2 {
		t.Errorf("expected count=2, got %v", m["count"])
	}

	// chapter=4 → 只 fs1
	args, _ = json.Marshal(map[string]int{"chapter": 4})
	res, err = tool.Handler(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = res.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("expected count=1 (chapter<=4), got %v", m["count"])
	}
}

// TestTimelineQueryTool 测 timeline_query 查 chapter 范围摘要.
func TestTimelineQueryTool(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, MemoryConfig{
		CoreTokenBudget: 3000, CharacterTokenBudget: 4000, RecentChapterCount: 5, EventTopK: 8,
	})
	if _, err := mm.Tracker().Init("test"); err != nil {
		t.Fatal(err)
	}

	state, _ := mm.LoadState(mm.projectName())
	state.RecentChapterSummaries[1] = "第一章摘要"
	state.RecentChapterSummaries[2] = "第二章摘要"
	state.RecentChapterSummaries[3] = "第三章摘要"
	if err := mm.Tracker().Write(state); err != nil {
		t.Fatal(err)
	}

	tools := Tools(mm)
	tool := tools[2] // timeline_query
	ctx := context.Background()

	args, _ := json.Marshal(map[string]int{"from": 2, "to": 3})
	res, err := tool.Handler(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m["count"] != 2 {
		t.Errorf("expected count=2, got %v", m["count"])
	}
	entries, _ := m["entries"].([]map[string]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries")
	}
	if entries[0]["chapter"] != 2 {
		t.Errorf("expected first entry chapter=2, got %v", entries[0]["chapter"])
	}
}

// TestGraphQueryTool 测 graph_query 查 from→to 路径.
func TestGraphQueryTool(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, MemoryConfig{
		CoreTokenBudget: 3000, CharacterTokenBudget: 4000, RecentChapterCount: 5, EventTopK: 8,
	})
	if _, err := mm.Tracker().Init("test"); err != nil {
		t.Fatal(err)
	}

	// 加 3 个节点 + 2 条边
	// 先 LoadState 初始化 m.graph (load 完 state 才有 graph)
	if _, err := mm.Tracker().Init("test"); err != nil {
		t.Fatal(err)
	}
	if _, err := mm.LoadState(mm.projectName()); err != nil {
		t.Fatal(err)
	}
	mm.Graph().AddNode(GraphNode{ID: "char:A", Type: NodeCharacter, Name: "A"})
	mm.Graph().AddNode(GraphNode{ID: "char:B", Type: NodeCharacter, Name: "B"})
	mm.Graph().AddNode(GraphNode{ID: "char:C", Type: NodeCharacter, Name: "C"})
	mm.Graph().AddEdge(GraphEdge{FromID: "char:A", ToID: "char:B", Type: EdgeType("friend")})
	mm.Graph().AddEdge(GraphEdge{FromID: "char:B", ToID: "char:C", Type: EdgeType("friend")})

	tools := Tools(mm)
	tool := tools[0] // graph_query
	ctx := context.Background()

	// A→C 应该能找到 1 条路径 (A→B→C)
	args, _ := json.Marshal(map[string]string{"from": "char:A", "to": "char:C"})
	res, err := tool.Handler(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m["count"].(int) == 0 {
		t.Errorf("expected at least 1 path A→C, got 0")
	}
}
