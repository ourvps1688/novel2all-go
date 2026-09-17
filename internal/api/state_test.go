package api

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestStoreAndPersistor 创建临时 project store + persistor
func newTestStoreAndPersistor(t *testing.T) (*ProjectStore, *StatePersistor, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewProjectStore()
	p := NewStatePersistor(path, store)
	return store, p, path
}

func TestStatePersistor_SaveLoadRoundTrip(t *testing.T) {
	store, p, path := newTestStoreAndPersistor(t)

	// 1. 创建 2 个 projects
	_, err := store.Create("Project Alpha", "alpha", "First project", 1, "fantasy")
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	p2, err := store.Create("Project Beta", "beta", "Second project", 1, "scifi")
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}
	_ = p2

	// 2. 增加一些 cache counters
	atomicStoreInt64(&cacheHits, 100)
	atomicStoreInt64(&cacheMisses, 20)
	atomicStoreInt64(&prefixHits, 50)
	atomicStoreInt64(&prefixMisses, 10)
	atomicStoreInt64(&cacheSize, 200)

	// 3. 保存
	if err := p.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// 4. 验证 file 存在
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	// 5. 创建新的 store + persistor (模拟重启)
	store2 := NewProjectStore()
	p2Persist := NewStatePersistor(path, store2)

	// 6. Load
	if err := p2Persist.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	// 7. 验证 projects 已恢复
	projects := store2.List()
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	// 验证 ID 保留
	foundAlpha := false
	for _, proj := range projects {
		if proj.Slug == "alpha" && proj.Name == "Project Alpha" {
			foundAlpha = true
		}
	}
	if !foundAlpha {
		t.Error("project Alpha not found after reload")
	}

	// 8. 验证 cache counters 已恢复
	if atomicLoadInt64(&cacheHits) != 100 {
		t.Errorf("expected cache_hits=100, got %d", atomicLoadInt64(&cacheHits))
	}
	if atomicLoadInt64(&cacheMisses) != 20 {
		t.Errorf("expected cache_misses=20, got %d", atomicLoadInt64(&cacheMisses))
	}
	if atomicLoadInt64(&prefixHits) != 50 {
		t.Errorf("expected prefix_hits=50, got %d", atomicLoadInt64(&prefixHits))
	}
	if atomicLoadInt64(&cacheSize) != 200 {
		t.Errorf("expected cache_size=200, got %d", atomicLoadInt64(&cacheSize))
	}
}

func TestStatePersistor_LoadNonExistent(t *testing.T) {
	// 文件不存在的场景应该返回 nil (静默)
	_, p, _ := newTestStoreAndPersistor(t)
	// 不调用 Save,直接 Load
	if err := p.Load(); err != nil {
		t.Errorf("expected nil for non-existent file, got %v", err)
	}
}

func TestStatePersistor_LoadUnsupportedVersion(t *testing.T) {
	store, p, path := newTestStoreAndPersistor(t)

	// 写一个 version=99 的 state file
	badData := `{"version": 99, "saved_at": "2026-09-17T10:00:00Z", "projects": [], "cache": {}}`
	if err := os.WriteFile(path, []byte(badData), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	// Load 应该返回错误
	err := p.Load()
	if err == nil {
		t.Fatal("expected error for unsupported version, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported state version") {
		t.Errorf("expected 'unsupported state version' in error, got: %v", err)
	}

	// 验证内存未被污染
	projects := store.List()
	if len(projects) != 0 {
		t.Errorf("expected 0 projects after failed load, got %d", len(projects))
	}
}

func TestStatePersistor_Reset(t *testing.T) {
	store, p, path := newTestStoreAndPersistor(t)

	// 创建 1 个 project
	_, err := store.Create("To Be Deleted", "delete-me", "", 1, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 设置 cache counters
	atomicStoreInt64(&cacheHits, 50)

	// 保存
	if err := p.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reset
	deleted, err := p.Reset()
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !deleted {
		t.Error("expected file to be deleted")
	}

	// 验证 file 已删除
	if _, err := os.Stat(path); err == nil {
		t.Error("state file should be deleted after reset")
	}

	// 验证内存清空
	projects := store.List()
	if len(projects) != 0 {
		t.Errorf("expected 0 projects after reset, got %d", len(projects))
	}
	if atomicLoadInt64(&cacheHits) != 0 {
		t.Errorf("expected cache_hits=0 after reset, got %d", atomicLoadInt64(&cacheHits))
	}
}

func TestStatePersistor_Info(t *testing.T) {
	_, p, path := newTestStoreAndPersistor(t)

	// Info on non-existent file
	info := p.Info()
	if info.Exists {
		t.Error("expected Exists=false before save")
	}

	// Save
	if err := p.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Info after save
	info = p.Info()
	if !info.Exists {
		t.Error("expected Exists=true after save")
	}
	if info.Size == 0 {
		t.Error("expected non-zero file size after save")
	}
	if info.Path != path {
		t.Errorf("expected Path=%q, got %q", path, info.Path)
	}
	if info.LastSavedAt.IsZero() {
		t.Error("expected non-zero LastSavedAt after save")
	}
}

func TestStatePersistor_AtomicWrite(t *testing.T) {
	// 验证写失败的回滚（写入失败时不应产生半写文件）
	_, _, path := newTestStoreAndPersistor(t)
	store := NewProjectStore()
	p := NewStatePersistor(path, store)

	// 第一次写入成功
	if err := p.Save(); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// 第二次写入覆盖
	_, _ = store.Create("After", "after", "", 1, "")
	if err := p.Save(); err != nil {
		t.Fatalf("second save: %v", err)
	}

	// 验证 .tmp 不存在
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf("temp file %s should not exist after successful save", tmpPath)
	}
}

func TestStatePersistor_ConcurrentSave(t *testing.T) {
	// 验证并发 Save 不会损坏文件（race detector 检查）
	_, p, _ := newTestStoreAndPersistor(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Save(); err != nil {
				t.Errorf("concurrent save: %v", err)
			}
		}()
	}
	wg.Wait()
}
