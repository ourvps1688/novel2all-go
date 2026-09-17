// rollback_test.go 测试 RollbackManager.
package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRollbackManager_SnapshotAndLoad(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManager(dir, 0)

	state := newEmptyState("test")
	state.Characters["alice"] = CharacterState{Name: "alice"}

	snap, err := rm.Snapshot(5, state)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Chapter != 5 {
		t.Errorf("chapter = %d, want 5", snap.Chapter)
	}
	if snap.SnapshotPath == "" {
		t.Error("snapshot_path empty")
	}

	// file should exist
	if _, err := os.Stat(snap.SnapshotPath); err != nil {
		t.Error("snapshot file not created")
	}

	// load back
	loaded, err := rm.LoadSnapshot(5)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Characters["alice"].Name != "alice" {
		t.Error("roundtrip failed")
	}
}

func TestRollbackManager_Rollback(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManager(dir, 0)
	tracker := NewTracker(filepath.Join(dir, "data", "_tracking-state.json"))

	// init state
	state, _ := tracker.Read()
	state.Characters["alice"] = CharacterState{Name: "alice"}
	if err := tracker.Write(state); err != nil {
		t.Fatal(err)
	}

	// snapshot before modification
	snap, err := rm.Snapshot(1, state)
	if err != nil {
		t.Fatal(err)
	}

	// modify state
	state.Characters["bob"] = CharacterState{Name: "bob"}
	alice := state.Characters["alice"]
	alice.Location = "modified"
	state.Characters["alice"] = alice
	if err := tracker.Write(state); err != nil {
		t.Fatal(err)
	}

	// verify modification
	current, _ := tracker.Read()
	if current.Characters["alice"].Location != "modified" {
		t.Error("modification not saved")
	}

	// rollback
	result, err := rm.Rollback(snap, tracker)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Restored {
		t.Error("rollback should succeed")
	}

	// verify state restored
	restored, _ := tracker.Read()
	if restored.Characters["alice"].Location != "" {
		t.Errorf("alice location = %q, want empty (restored)", restored.Characters["alice"].Location)
	}
	if _, ok := restored.Characters["bob"]; ok {
		t.Error("bob should not exist after rollback")
	}
}

func TestRollbackManager_ListSnapshots(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManager(dir, 0)

	for _, ch := range []int{3, 1, 5, 2, 4} {
		_, err := rm.Snapshot(ch, newEmptyState("test"))
		if err != nil {
			t.Fatal(err)
		}
	}

	chapters, err := rm.ListSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 5 {
		t.Errorf("count = %d, want 5", len(chapters))
	}
	// should be sorted ASC
	for i := 1; i < len(chapters); i++ {
		if chapters[i] < chapters[i-1] {
			t.Errorf("not sorted: %v", chapters)
		}
	}
}

func TestRollbackManager_CleanupOldBackups(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManager(dir, 3) // 保留最近 3 个

	for _, ch := range []int{1, 2, 3, 4, 5} {
		_, err := rm.Snapshot(ch, newEmptyState("test"))
		if err != nil {
			t.Fatal(err)
		}
	}

	chapters, _ := rm.ListSnapshots()
	if len(chapters) != 3 {
		t.Errorf("after cleanup, count = %d, want 3", len(chapters))
	}
	// should keep newest 3: 3, 4, 5
	if chapters[0] != 3 || chapters[2] != 5 {
		t.Errorf("kept wrong snapshots: %v", chapters)
	}
}

func TestRollbackManager_RollbackNil(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManager(dir, 0)
	tracker := NewTracker(filepath.Join(dir, "data", "_tracking-state.json"))

	_, err := rm.Rollback(nil, tracker)
	if err == nil {
		t.Error("nil snapshot should error")
	}
}

func TestRollbackManager_RecordStateChange(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManager(dir, 5)

	before := newEmptyState("p")
	before.Characters = map[string]CharacterState{
		"alice": {Name: "alice", Location: "town"},
	}

	after := newEmptyState("p")
	after.Characters = map[string]CharacterState{
		"alice": {Name: "alice", Location: "city"},
		"bob":   {Name: "bob", Location: "town"},
	}
	after.Foreshadowing = map[string]ForeshadowingState{
		"fs1": {ID: "fs1", Status: "active"},
	}

	// 调用 RecordStateChange 不应 panic, 输出到 stderr (V0 简化: 仅 log)
	rm.RecordStateChange(1, before, after)
}

func TestRollbackManager_RecordStateChange_NilState(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManager(dir, 5)
	// nil state 不应 panic, diff 为空
	rm.RecordStateChange(1, nil, newEmptyState("p"))
	rm.RecordStateChange(1, newEmptyState("p"), nil)
}

func TestComputeStateDiff(t *testing.T) {
	before := newEmptyState("p")
	after := newEmptyState("p")
	after.LastUpdatedChapter = 5
	after.Characters = map[string]CharacterState{
		"alice": {Name: "alice"},
		"bob":   {Name: "bob"},
	}
	after.Foreshadowing = map[string]ForeshadowingState{
		"fs1": {ID: "fs1", Status: "active"},
	}
	after.Timeline = []TimelineEvent{{Chapter: 1, Event: "start"}}
	after.RecentChapterSummaries[5] = "chapter 5 summary"

	diff := computeStateDiff(before, after)
	if diff == "" {
		t.Error("diff should not be empty")
	}
	for _, want := range []string{"LastUpdatedChapter", "Characters", "Foreshadowing", "Timeline", "chapter 5"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff missing %q: %s", want, diff)
		}
	}
}

func TestComputeStateDiff_NilState(t *testing.T) {
	if computeStateDiff(nil, nil) != "" {
		t.Error("nil both should return empty diff")
	}
	if computeStateDiff(newEmptyState("p"), nil) != "" {
		t.Error("nil after should return empty diff")
	}
}

func TestRollbackManager_CleanupOldBackups_Exported(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManager(dir, 5)

	// 创建 6 个 snapshots
	for i := 1; i <= 6; i++ {
		_, err := rm.Snapshot(i, newEmptyState("p"))
		if err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
	}

	// 调 exported CleanupOldBackups(2) → 只保留 2 个
	if err := rm.CleanupOldBackups(2); err != nil {
		t.Fatal(err)
	}

	chapters, _ := rm.ListSnapshots()
	if len(chapters) != 2 {
		t.Errorf("after CleanupOldBackups(2): count = %d, want 2", len(chapters))
	}
	// 应该保留最新的 2 个 (ch 5, 6)
	if chapters[0] != 5 || chapters[1] != 6 {
		t.Errorf("kept wrong snapshots: %v", chapters)
	}
}
