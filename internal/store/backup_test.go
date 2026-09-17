package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestBackupManager 创建临时 backup manager + 临时 state.json + 临时 sqlite
func newTestBackupManager(t *testing.T, maxBackups int) (*BackupManager, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	stateDir := filepath.Join(dir, "state")
	_ = os.MkdirAll(stateDir, 0o755)

	statePath := filepath.Join(stateDir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"version":1,"projects":[]}`), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	dbPath := filepath.Join(stateDir, "test.db")
	if err := os.WriteFile(dbPath, []byte("fake sqlite content"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	mgr := NewBackupManager(backupDir, statePath, dbPath, maxBackups)
	return mgr, backupDir, statePath, dbPath
}

func TestBackupManager_Create(t *testing.T) {
	mgr, backupDir, _, _ := newTestBackupManager(t, 10)

	result, err := mgr.Create()
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if result.Filename == "" {
		t.Error("expected non-empty filename")
	}
	if !strings.HasPrefix(result.Filename, "novel2all-") {
		t.Errorf("expected filename prefix novel2all-, got %q", result.Filename)
	}
	if !strings.HasSuffix(result.Filename, ".tar.gz") {
		t.Errorf("expected .tar.gz suffix, got %q", result.Filename)
	}
	if result.Size <= 0 {
		t.Errorf("expected non-zero size, got %d", result.Size)
	}
	if !strings.HasPrefix(result.Path, backupDir) {
		t.Errorf("expected path in %s, got %s", backupDir, result.Path)
	}

	// file should exist
	if _, err := os.Stat(result.Path); err != nil {
		t.Errorf("backup file not created: %v", err)
	}
}

func TestBackupManager_Create_NoDB(t *testing.T) {
	// 没有 dbPath（dbPath=""）也应能成功
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	statePath := filepath.Join(dir, "state.json")
	_ = os.WriteFile(statePath, []byte(`{"version":1}`), 0o644)

	mgr := NewBackupManager(backupDir, statePath, "", 10)
	result, err := mgr.Create()
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.Filename == "" {
		t.Error("expected non-empty filename")
	}
}

func TestBackupManager_List(t *testing.T) {
	mgr, _, _, _ := newTestBackupManager(t, 10)

	// 创建 3 个 backup
	for i := 0; i < 3; i++ {
		if _, err := mgr.Create(); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	backups, err := mgr.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(backups) != 3 {
		t.Errorf("expected 3 backups, got %d", len(backups))
	}
	// 验证倒序（最新在前）
	for i := 0; i < len(backups)-1; i++ {
		if backups[i].CreatedAt.Before(backups[i+1].CreatedAt) {
			t.Errorf("backups not sorted desc: %v before %v", backups[i].CreatedAt, backups[i+1].CreatedAt)
		}
	}
}

func TestBackupManager_List_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	mgr := NewBackupManager(filepath.Join(dir, "nonexistent"), "", "", 10)

	backups, err := mgr.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("expected 0 backups, got %d", len(backups))
	}
}

func TestBackupManager_Cleanup_MaxBackups(t *testing.T) {
	// maxBackups=2, 创建 4 个, 应该保留 2 个
	mgr, _, _, _ := newTestBackupManager(t, 2)

	for i := 0; i < 4; i++ {
		if _, err := mgr.Create(); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	backups, err := mgr.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(backups) != 2 {
		t.Errorf("expected 2 backups after cleanup, got %d", len(backups))
	}
}

func TestBackupManager_Cleanup_ZeroKeepsAll(t *testing.T) {
	// maxBackups=0 表示保留全部
	mgr, _, _, _ := newTestBackupManager(t, 0)

	for i := 0; i < 5; i++ {
		if _, err := mgr.Create(); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	backups, err := mgr.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(backups) != 5 {
		t.Errorf("expected 5 backups (no cleanup), got %d", len(backups))
	}
}

func TestBackupManager_AtomicWrite(t *testing.T) {
	// 验证 .tmp 文件在成功后被清理
	mgr, _, _, _ := newTestBackupManager(t, 10)

	result, err := mgr.Create()
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// .tmp 不应存在
	dir := filepath.Dir(result.Path)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file should not exist: %s", e.Name())
		}
	}
}

func TestBackupManager_ManifestContents(t *testing.T) {
	mgr, _, _, _ := newTestBackupManager(t, 10)
	result, err := mgr.Create()
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	manifest, err := mgr.readManifest(result.Path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	// 应该包含 state.json 和 db.sqlite
	hasState := false
	hasDB := false
	for _, c := range manifest.Contents {
		if c == "state.json" {
			hasState = true
		}
		if c == "db.sqlite" {
			hasDB = true
		}
	}
	if !hasState {
		t.Error("manifest missing state.json")
	}
	if !hasDB {
		t.Error("manifest missing db.sqlite")
	}
	if len(manifest.Checksums) == 0 {
		t.Error("manifest should have checksums")
	}
}

func TestBackupManager_ChecksumCorrect(t *testing.T) {
	mgr, _, statePath, dbPath := newTestBackupManager(t, 10)
	result, err := mgr.Create()
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	manifest, _ := mgr.readManifest(result.Path)
	stateSum := sha256.Sum256(mustRead(t, statePath))
	expected := hex.EncodeToString(stateSum[:])
	if manifest.Checksums["state.json"] != expected {
		t.Errorf("state.json checksum mismatch: got %s, want %s", manifest.Checksums["state.json"], expected)
	}
	dbSum := sha256.Sum256(mustRead(t, dbPath))
	expected = hex.EncodeToString(dbSum[:])
	if manifest.Checksums["db.sqlite"] != expected {
		t.Errorf("db.sqlite checksum mismatch: got %s, want %s", manifest.Checksums["db.sqlite"], expected)
	}
}

// mustRead helper
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// 防止 unused 警告
var _ = context.Background
