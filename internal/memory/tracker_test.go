// tracker_test.go 测试 Tracker.
package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTracker_Exists(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "state.json")
	tr := NewTracker(fp)
	if tr.Exists() {
		t.Error("Exists should be false for new file")
	}
	// create empty file
	if err := os.WriteFile(fp, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !tr.Exists() {
		t.Error("Exists should be true after file created")
	}
}

func TestTracker_ReadEmpty(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker(filepath.Join(dir, "state.json"))
	state, err := tr.Read()
	if err != nil {
		t.Errorf("Read empty should not error: %v", err)
	}
	if state == nil {
		t.Fatal("Read should return non-nil empty state")
	}
	if state.SchemaVersion != 2 {
		t.Errorf("default SchemaVersion = %d, want 2", state.SchemaVersion)
	}
	if state.Characters == nil || state.Foreshadowing == nil {
		t.Error("empty state should have empty (not nil) maps")
	}
}

func TestTracker_WriteRead(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "state.json")
	tr := NewTracker(fp)

	state := newEmptyState("test-project")
	state.Characters["林雷"] = CharacterState{
		Name:               "林雷",
		Location:           "苍茫镇",
		EmotionalState:     "迷茫",
		LastUpdatedChapter: 1,
	}
	state.Foreshadowing["fs:bloodline"] = ForeshadowingState{
		ID: "fs:bloodline", Description: "血脉之谜", PlantedChapter: 1, Status: "active",
	}

	if err := tr.Write(state); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// read back
	state2, err := tr.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if state2.ProjectName != "test-project" {
		t.Errorf("ProjectName = %q, want test-project", state2.ProjectName)
	}
	if state2.Characters["林雷"].Location != "苍茫镇" {
		t.Errorf("林雷.Location = %q, want 苍茫镇", state2.Characters["林雷"].Location)
	}
	if state2.Foreshadowing["fs:bloodline"].Description != "血脉之谜" {
		t.Error("foreshadowing roundtrip failed")
	}
}

func TestTracker_WriteAtomic(t *testing.T) {
	// 验证 write 用 tmp + rename (atomic on POSIX)
	dir := t.TempDir()
	fp := filepath.Join(dir, "state.json")
	tr := NewTracker(fp)

	if err := tr.Write(newEmptyState("v1")); err != nil {
		t.Fatal(err)
	}
	if err := tr.Write(newEmptyState("v2")); err != nil {
		t.Fatal(err)
	}
	// tmp file should not exist after rename
	if _, err := os.Stat(fp + ".tmp"); err == nil {
		t.Error("tmp file should be cleaned up after rename")
	}
	state, _ := tr.Read()
	if state.ProjectName != "v2" {
		t.Errorf("ProjectName = %q, want v2", state.ProjectName)
	}
}

func TestTracker_GetCharacter(t *testing.T) {
	state := newEmptyState("test")
	state.Characters["林雷"] = CharacterState{Name: "林雷", Location: "镇东"}

	tr := &Tracker{}
	c, ok := tr.GetCharacter(state, "林雷")
	if !ok {
		t.Fatal("林雷 should exist")
	}
	if c.Location != "镇东" {
		t.Errorf("Location = %q", c.Location)
	}
	_, ok = tr.GetCharacter(state, "不存在")
	if ok {
		t.Error("nonexistent character should return false")
	}
}

func TestTracker_GetActiveForeshadowing(t *testing.T) {
	state := newEmptyState("test")
	state.Foreshadowing["fs1"] = ForeshadowingState{ID: "fs1", Status: "active"}
	state.Foreshadowing["fs2"] = ForeshadowingState{ID: "fs2", Status: "revealed"}
	state.Foreshadowing["fs3"] = ForeshadowingState{ID: "fs3", Status: "active"}

	tr := &Tracker{}
	active := tr.GetActiveForeshadowing(state)
	if len(active) != 2 {
		t.Errorf("active count = %d, want 2", len(active))
	}
}

func TestTracker_GetRecentSummaries(t *testing.T) {
	state := newEmptyState("test")
	state.RecentChapterSummaries[1] = "chapter 1"
	state.RecentChapterSummaries[2] = "chapter 2"
	state.RecentChapterSummaries[3] = "chapter 3"
	state.RecentChapterSummaries[5] = "chapter 5"

	tr := &Tracker{}
	all := tr.GetRecentSummaries(state, 0)
	if len(all) != 4 {
		t.Errorf("all count = %d, want 4", len(all))
	}

	top2 := tr.GetRecentSummaries(state, 2)
	if len(top2) != 2 {
		t.Errorf("top2 count = %d, want 2", len(top2))
	}
	if top2[0].Chapter != 5 {
		t.Errorf("top2[0] chapter = %d, want 5 (DESC)", top2[0].Chapter)
	}
	if top2[1].Chapter != 3 {
		t.Errorf("top2[1] chapter = %d, want 3", top2[1].Chapter)
	}
}

func TestTracker_JSON(t *testing.T) {
	state := newEmptyState("json-test")
	state.Characters["x"] = CharacterState{Name: "x"}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TrackingState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Characters["x"].Name != "x" {
		t.Error("roundtrip failed")
	}
}
