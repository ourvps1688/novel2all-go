// rollback_test.go 测试 RollbackManager.
package memory

import (
	"os"
	"path/filepath"
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
	tracker.Write(state)

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
	_, _ = tracker.Write(state)

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
