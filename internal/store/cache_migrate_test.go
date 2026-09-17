package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONCache_SetGet(t *testing.T) {
	dir := t.TempDir()
	c, err := NewJSONCache(filepath.Join(dir, "test.json"), 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Set("k1", "v1"); err != nil {
		t.Fatal(err)
	}
	if v := c.Get("k1"); v != "v1" {
		t.Errorf("Get(k1) = %v, want v1", v)
	}
}

func TestJSONCache_Keys(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewJSONCache(filepath.Join(dir, "t.json"), 100, 0)
	defer c.Close()
	_ = c.Set("a", 1)
	_ = c.Set("b", 2)
	_ = c.Set("c", 3)
	keys := c.Keys()
	if len(keys) != 3 {
		t.Errorf("Keys count = %d, want 3", len(keys))
	}
}

func TestJSONCache_TTL(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewJSONCache(filepath.Join(dir, "t.json"), 100, 1) // TTL 1 second
	defer c.Close()
	_ = c.Set("k", "v")
	// 不等, 应该立即能 Get
	if v := c.Get("k"); v != "v" {
		t.Error("expected immediate read")
	}
}

func TestJSONCache_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.json")
	c1, _ := NewJSONCache(path, 100, 0)
	_ = c1.Set("persist", "yes")
	c1.Close()

	c2, _ := NewJSONCache(path, 100, 0)
	defer c2.Close()
	if v := c2.Get("persist"); v != "yes" {
		t.Errorf("persistence failed, got %v", v)
	}
}

func TestJSONCache_LRUEviction(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewJSONCache(filepath.Join(dir, "t.json"), 2, 0) // maxSize=2
	defer c.Close()
	_ = c.Set("a", 1)
	_ = c.Set("b", 2)
	_ = c.Set("c", 3) // 应该淘汰一个
	if c.Size() > 2 {
		t.Errorf("Size = %d, want <= 2", c.Size())
	}
}

func TestMigrateCache_JsonToJson(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.json")
	dstPath := filepath.Join(dir, "dst.json")
	src, _ := NewJSONCache(srcPath, 100, 0)
	defer src.Close()
	_ = src.Set("k1", "v1")
	_ = src.Set("k2", 42)
	_ = src.Set("k3", map[string]any{"nested": true})

	result, err := MigrateCache("json", "json", srcPath, dstPath, 100, 0, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result.TotalEntries != 3 {
		t.Errorf("TotalEntries = %d, want 3", result.TotalEntries)
	}
	if result.Migrated != 3 {
		t.Errorf("Migrated = %d, want 3", result.Migrated)
	}
	// 重新打开 dst 验证持久化数据
	dst, _ := NewJSONCache(dstPath, 100, 0)
	defer dst.Close()
	if v := dst.Get("k1"); v != "v1" {
		t.Errorf("dst.Get(k1) = %v, want v1", v)
	}
	// 注意: int 经 JSON roundtrip 后变 float64, 这是 JSON 标准行为
	if v := dst.Get("k2"); v != float64(42) {
		t.Errorf("dst.Get(k2) = %v (%T), want float64(42)", v, v)
	}
}

func TestMigrateCache_EmptySrc(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.json")
	dstPath := filepath.Join(dir, "dst.json")
	src, _ := NewJSONCache(srcPath, 100, 0)
	defer src.Close()

	result, err := MigrateCache("json", "json", srcPath, dstPath, 100, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalEntries != 0 {
		t.Errorf("TotalEntries = %d, want 0", result.TotalEntries)
	}
}

func TestMigrateCache_ProgressCallback(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.json")
	dstPath := filepath.Join(dir, "dst.json")
	src, _ := NewJSONCache(srcPath, 100, 0)
	defer src.Close()
	for i := 0; i < 60; i++ {
		_ = src.Set(string(rune('a'+i%26))+"_"+string(rune('0'+i/26)), i)
	}

	var calls int
	result, err := MigrateCache("json", "json", srcPath, dstPath, 100, 0,
		func(done, total int) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	if result.Migrated != 60 {
		t.Errorf("Migrated = %d, want 60", result.Migrated)
	}
	if calls == 0 {
		t.Error("progress callback never called")
	}
}

func TestMigrateCache_UnsupportedBackend(t *testing.T) {
	_, err := MigrateCache("redis", "json", "/tmp/a", "/tmp/b", 100, 0, nil)
	if err == nil {
		t.Error("expected error for unsupported backend")
	}
}

func TestMigrateResult_Summary(t *testing.T) {
	r := &MigrationResult{
		SrcBackend:     "json",
		DstBackend:     "sqlite",
		TotalEntries:   10,
		Migrated:       10,
		SkippedExpired: 0,
		Errors:         nil,
		ElapsedSeconds: 1.5,
	}
	s := r.Summary()
	if !contains(s, "json") || !contains(s, "sqlite") || !contains(s, "10") {
		t.Errorf("summary missing fields: %s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ensure unused import avoided
var _ = json.Marshal
var _ = os.Create
