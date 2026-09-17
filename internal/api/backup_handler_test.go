package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

func newTestBackupHandler(t *testing.T) (*BackupHandler, string) {
	t.Helper()
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	statePath := filepath.Join(dir, "state.json")
	dbPath := filepath.Join(dir, "test.db")
	_ = os.WriteFile(statePath, []byte(`{"version":1}`), 0o644)
	_ = os.WriteFile(dbPath, []byte("fake"), 0o644)

	mgr := store.NewBackupManager(backupDir, statePath, dbPath, 10)
	session := newMockLookup("admin-token", "user-token")
	return NewBackupHandler(mgr, session), backupDir
}

func TestBackup_Create_RequiresAuth(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/backup", http.NoBody)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestBackup_Create_RequiresAdmin(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/backup", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestBackup_Create_AdminSuccess(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/backup", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
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

func TestBackup_List_AdminSuccess(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	// 先创建 2 个 backup
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/backup", http.NoBody)
		req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup create %d failed: %d", i, rr.Code)
		}
	}

	// 列出
	req := httptest.NewRequest(http.MethodGet, "/api/backup", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
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

func TestBackup_List_RequiresAdmin(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/backup", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", rr.Code)
	}
}

func TestBackup_MethodNotAllowed(t *testing.T) {
	h, _ := newTestBackupHandler(t)

	// PUT /api/backup → 405
	req := httptest.NewRequest(http.MethodPut, "/api/backup", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}

	// POST /api/backup/unknown → 404
	req = httptest.NewRequest(http.MethodPost, "/api/backup/unknown", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown subpath, got %d", rr.Code)
	}
}

func TestBackup_NilManagerReturns503(t *testing.T) {
	session := newMockLookup("admin-token", "user-token")
	h := NewBackupHandler(nil, session)

	req := httptest.NewRequest(http.MethodPost, "/api/backup", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// nil manager 会 panic in Create, 应该 500
	// (但 requireAdmin 通过, handler 进入 handleCreate)
	if rr.Code != http.StatusInternalServerError && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 500/503 with nil manager, got %d", rr.Code)
	}
}
