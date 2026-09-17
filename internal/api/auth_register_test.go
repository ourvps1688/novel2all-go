package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

func setupTestAuthWithAdmin(t *testing.T) (*AuthHandler, *store.DB) {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 创建 admin + 普通用户
	adminHash, _ := auth.HashPassword("adminpass")
	if _, err := db.CreateUser(ctx, "admin", adminHash, "admin"); err != nil {
		t.Fatal(err)
	}
	userHash, _ := auth.HashPassword("userpass")
	if _, err := db.CreateUser(ctx, "alice", userHash, "user"); err != nil {
		t.Fatal(err)
	}

	sm := auth.NewSessionManager(db, auth.DefaultSessionConfig())
	limiter := auth.NewRateLimiter(5, 1*time.Minute, 30*time.Second)
	return NewAuthHandler(sm, limiter), db
}

// TestRegister_Basic 测试基本注册
func TestRegister_Basic(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	body, _ := json.Marshal(RegisterRequest{
		Username: "newuser1",
		Password: "pass1234",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("期望 201，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp RegisterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Username != "newuser1" {
		t.Errorf("username=%q", resp.Username)
	}
	if resp.Role != "user" {
		t.Errorf("role 应为 user（register 强制）=%q", resp.Role)
	}
}

// TestRegister_DuplicateUsername 测试重名返回 409
func TestRegister_DuplicateUsername(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	body, _ := json.Marshal(RegisterRequest{Username: "alice", Password: "pass1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("重名应 409，实际=%d", rec.Code)
	}
}

// TestRegister_ShortPassword 测试密码太短
func TestRegister_ShortPassword(t *testing.T) {
	h, _ := setupTestAuthWithAdmin(t)

	body, _ := json.Marshal(RegisterRequest{Username: "newuser2", Password: "123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("短密码应 400，实际=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestRolesList 测试 /api/roles
func TestRolesList(t *testing.T) {
	h := NewRolesHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/roles", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d", rec.Code)
	}
	var resp RolesListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 5 {
		t.Errorf("应 5 个角色，实际=%d", resp.Count)
	}
}

// TestCacheStats 测试 cache 统计
func TestCacheStats(t *testing.T) {
	h := NewCacheHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/cache/stats", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var stats CacheStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.Backend != "memory" {
		t.Errorf("backend 应为 memory，实际=%q", stats.Backend)
	}
}

// TestPromptCacheStats 测试 prompt prefix cache 统计
func TestPromptCacheStats(t *testing.T) {
	h := NewCacheHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/cache/prompt-stats", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var stats PromptCacheStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.HitRate != 0 {
		t.Errorf("初始 hit_rate 应为 0，实际=%f", stats.HitRate)
	}
}
