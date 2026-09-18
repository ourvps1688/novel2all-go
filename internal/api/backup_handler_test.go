package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// newTestBackupHandler 创建临时 backup handler (Sprint V1.0.1 P3: 移除 session 参数).
func newTestBackupHandler(t *testing.T) (*BackupHandler, string) {
	t.Helper()
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	statePath := filepath.Join(dir, "state.json")
	dbPath := filepath.Join(dir, "test.db")
	_ = os.WriteFile(statePath, []byte(`{"version":1}`), 0o644)
	_ = os.WriteFile(dbPath, []byte("fake"), 0o644)

	mgr := store.NewBackupManager(backupDir, statePath, dbPath, 10)
	return NewBackupHandler(mgr), backupDir
}

// backupReqAdmin 构造带 admin user context 的 request.
func backupReqAdmin(method, url string, body []byte) *http.Request {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, url, bodyReader)
	admin := &store.User{ID: 999, Role: "admin", Username: "test-admin"}
	ctx := context.WithValue(req.Context(), userCtxValue, admin)
	return req.WithContext(ctx)
}

func TestBackup_Create_OK(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	req := backupReqAdmin(http.MethodPost, "/api/backup", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var result store.BackupResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.Filename == "" {
		t.Error("expected non-empty filename")
	}
	if result.Size <= 0 {
		t.Errorf("expected non-zero size, got %d", result.Size)
	}
}

func TestBackup_List_OK(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	// 先创建 2 个 backup
	for i := 0; i < 2; i++ {
		req := backupReqAdmin(http.MethodPost, "/api/backup", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup create %d failed: %d", i, rr.Code)
		}
	}

	// 列出
	req := backupReqAdmin(http.MethodGet, "/api/backup", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Backups []store.BackupInfo `json:"backups"`
		Count   int                `json:"count"`
		Dir     string             `json:"dir"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("expected count=2, got %d", resp.Count)
	}
	if len(resp.Backups) != 2 {
		t.Errorf("expected 2 backups, got %d", len(resp.Backups))
	}
}

func TestBackup_MethodNotAllowed(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	// PUT /api/backup → 405
	req := backupReqAdmin(http.MethodPut, "/api/backup", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}

	// POST /api/backup/unknown → 404
	req = backupReqAdmin(http.MethodPost, "/api/backup/unknown", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown subpath, got %d", rr.Code)
	}
}

func TestBackup_NilManagerReturns503(t *testing.T) {
	// Sprint V1.0.1 P3: 仍保留 "nil manager → 503" 测试 (handler 防御性检查).
	h := NewBackupHandler(nil)

	req := backupReqAdmin(http.MethodPost, "/api/backup", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with nil manager, got %d", rr.Code)
	}
}
