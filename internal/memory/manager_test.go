// manager_test.go 测试 MemoryManager.
package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMemoryManager_LoadState_Empty(t *testing.T) {
	dir := t.TempDir()
	m := NewMemoryManager(dir, nil, DefaultMemoryConfig())
	state, err := m.LoadState("test-project")
	if err != nil {
		t.Fatal(err)
	}
	if state.ProjectName != "test-project" {
		t.Errorf("ProjectName = %q", state.ProjectName)
	}
	// 应该创建 state 文件
	if !m.Tracker().Exists() {
		t.Error("state file should be created on LoadState")
	}
}

func TestMemoryManager_LoadState_Existing(t *testing.T) {
	dir := t.TempDir()
	// 先写一个 state
	tr := NewTracker(filepath.Join(dir, "data", "_tracking-state.json"))
	state := newEmptyState("existing-project")
	state.Characters["alice"] = CharacterState{Name: "alice", Location: "town"}
	if err := tr.Write(state); err != nil {
		t.Fatal(err)
	}

	m := NewMemoryManager(dir, nil, DefaultMemoryConfig())
	loaded, err := m.LoadState("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProjectName != "existing-project" {
		t.Errorf("should preserve existing project_name, got %q", loaded.ProjectName)
	}
	if loaded.Characters["alice"].Location != "town" {
		t.Error("existing characters should be preserved")
	}
}

func TestMemoryManager_LoadForWriting_CoreSettings(t *testing.T) {
	dir := t.TempDir()
	// 创建核心设定文件
	if err := writeFile(filepath.Join(dir, "创作设定.md"), []byte("魔法体系设定")); err != nil {
		t.Fatal(err)
	}
	mkdir(t, filepath.Join(dir, "设定"))
	if err := writeFile(filepath.Join(dir, "设定", "文风.md"), []byte("古风古韵")); err != nil {
		t.Fatal(err)
	}

	m := NewMemoryManager(dir, nil, DefaultMemoryConfig())
	_, _ = m.LoadState("test")

	ctx := context.Background()
	memCtx, err := m.LoadForWriting(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(memCtx.Core) != 2 {
		t.Errorf("expected 2 core items, got %d", len(memCtx.Core))
	}
	// verify content
	found := false
	for _, item := range memCtx.Core {
		if item.Content == "魔法体系设定" {
			found = true
		}
	}
	if !found {
		t.Error("magic system setting not in core")
	}
}

func TestMemoryManager_LoadForWriting_RecentSummaries(t *testing.T) {
	dir := t.TempDir()
	m := NewMemoryManager(dir, nil, DefaultMemoryConfig())
	state, _ := m.LoadState("test")
	// 注入 recent summaries
	state.RecentChapterSummaries[1] = "first chapter summary"
	state.RecentChapterSummaries[2] = "second chapter summary"
	state.RecentChapterSummaries[5] = "fifth chapter summary"
	if err := m.Tracker().Write(state); err != nil {
		t.Fatal(err)
	}
	m.graph = FromState(state)

	memCtx, err := m.LoadForWriting(context.Background(), 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(memCtx.Recent) != 3 {
		t.Errorf("recent count = %d, want 3", len(memCtx.Recent))
	}
	// verify DESC order (5 first, then 2, then 1)
	if memCtx.Recent[0].Layer != LayerRecent {
		t.Error("layer wrong")
	}
}

func TestMemoryManager_UpdateAfterWriting_NoLLM(t *testing.T) {
	// 不传 LLM, UpdateAfterWriting 应仍然工作 (仅 summarizer)
	dir := t.TempDir()
	m := NewMemoryManager(dir, nil, DefaultMemoryConfig())
	_, _ = m.LoadState("test")

	content := "第一句话描述了情节。第二句话继续。第三句话。最后一句话结束。第五句话。"
	state, err := m.UpdateAfterWriting(context.Background(), 5, content)
	if err != nil {
		t.Fatal(err)
	}
	// summary 应该保存
	if _, ok := state.RecentChapterSummaries[5]; !ok {
		t.Error("chapter 5 summary should exist")
	}
}

func TestMemoryManager_PreWriteCheck_Stub(t *testing.T) {
	dir := t.TempDir()
	m := NewMemoryManager(dir, nil, DefaultMemoryConfig())
	_, _ = m.LoadState("test")

	issues, err := m.PreWriteCheck(context.Background(), "outline")
	if err != nil {
		t.Fatal(err)
	}
	// V0 stub: 返回空
	if len(issues) != 0 {
		t.Errorf("pre-write stub should return empty, got %d", len(issues))
	}
}

func TestMemoryManager_PostWriteCheck_Stub(t *testing.T) {
	dir := t.TempDir()
	m := NewMemoryManager(dir, nil, DefaultMemoryConfig())
	_, _ = m.LoadState("test")

	issues, err := m.PostWriteCheck(context.Background(), "content")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("post-write stub should return empty, got %d", len(issues))
	}
}

// writeFile 简单 wrapper.
func writeFile(fp string, data []byte) error {
	return writeFileOS(fp, data)
}

// mkdir 简单 wrapper.
func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := mkdirOS(dir); err != nil {
		t.Fatal(err)
	}
}
