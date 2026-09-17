package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func setupMembershipTest(t *testing.T) (*DB, int64, int64) {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 创建 2 个用户（admin + 普通用户）
	fakeHash := "fake_hash_for_test_purposes_only"
	adminID, err := db.CreateUser(ctx, "admin_user", fakeHash, "admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	normalID, err := db.CreateUser(ctx, "normal_user", fakeHash, "user")
	if err != nil {
		t.Fatalf("create normal: %v", err)
	}
	return db, adminID, normalID
}

func TestGrantProjectAccess_Success(t *testing.T) {
	db, adminID, normalID := setupMembershipTest(t)
	ctx := context.Background()

	m, err := db.GrantProjectAccess(ctx, normalID, "/path/to/proj", "editor", adminID)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if m.UserID != normalID || m.Role != "editor" {
		t.Errorf("membership wrong: %+v", m)
	}
}

func TestGrantProjectAccess_Duplicate(t *testing.T) {
	db, adminID, normalID := setupMembershipTest(t)
	ctx := context.Background()

	_, err := db.GrantProjectAccess(ctx, normalID, "/proj", "viewer", adminID)
	if err != nil {
		t.Fatalf("first grant: %v", err)
	}
	_, err = db.GrantProjectAccess(ctx, normalID, "/proj", "editor", adminID)
	if !errors.Is(err, ErrMembershipExists) {
		t.Errorf("expected ErrMembershipExists, got %v", err)
	}
}

func TestGetProjectRole_NotFound(t *testing.T) {
	db, _, normalID := setupMembershipTest(t)
	ctx := context.Background()

	role, err := db.GetProjectRole(ctx, normalID, "/nonexistent")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if role != "" {
		t.Errorf("role=%q, want empty", role)
	}
}

func TestGetProjectRole_Found(t *testing.T) {
	db, adminID, normalID := setupMembershipTest(t)
	ctx := context.Background()

	_, _ = db.GrantProjectAccess(ctx, normalID, "/proj1", "owner", adminID)
	role, err := db.GetProjectRole(ctx, normalID, "/proj1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if role != "owner" {
		t.Errorf("role=%q, want owner", role)
	}
}

func TestRevokeProjectAccess(t *testing.T) {
	db, adminID, normalID := setupMembershipTest(t)
	ctx := context.Background()

	_, _ = db.GrantProjectAccess(ctx, normalID, "/proj", "viewer", adminID)
	if err := db.RevokeProjectAccess(ctx, normalID, "/proj"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// 再撤销应该返回 not found
	if err := db.RevokeProjectAccess(ctx, normalID, "/proj"); !errors.Is(err, ErrMembershipNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestListProjectMembers(t *testing.T) {
	db, adminID, normalID := setupMembershipTest(t)
	ctx := context.Background()

	// 第二个用户
	otherID, _ := db.CreateUser(ctx, "other", "fake_hash", "user")

	_, _ = db.GrantProjectAccess(ctx, normalID, "/proj", "editor", adminID)
	_, _ = db.GrantProjectAccess(ctx, otherID, "/proj", "viewer", adminID)

	members, err := db.ListProjectMembers(ctx, "/proj")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("members=%d, want 2", len(members))
	}
}

func TestListUserProjects(t *testing.T) {
	db, adminID, normalID := setupMembershipTest(t)
	ctx := context.Background()

	_, _ = db.GrantProjectAccess(ctx, normalID, "/proj1", "editor", adminID)
	_, _ = db.GrantProjectAccess(ctx, normalID, "/proj2", "viewer", adminID)
	_, _ = db.GrantProjectAccess(ctx, normalID, "/proj3", "owner", adminID)

	projs, err := db.ListUserProjects(ctx, normalID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projs) != 3 {
		t.Errorf("projs=%d, want 3", len(projs))
	}
}

func TestUpdateProjectRole(t *testing.T) {
	db, adminID, normalID := setupMembershipTest(t)
	ctx := context.Background()

	_, _ = db.GrantProjectAccess(ctx, normalID, "/proj", "viewer", adminID)
	if err := db.UpdateProjectRole(ctx, normalID, "/proj", "editor"); err != nil {
		t.Fatalf("update: %v", err)
	}
	role, _ := db.GetProjectRole(ctx, normalID, "/proj")
	if role != "editor" {
		t.Errorf("role=%q, want editor", role)
	}
}

func TestUpdateProjectRole_NotFound(t *testing.T) {
	db, _, normalID := setupMembershipTest(t)
	ctx := context.Background()

	err := db.UpdateProjectRole(ctx, normalID, "/nonexistent", "editor")
	if !errors.Is(err, ErrMembershipNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestMembershipCascadeOnUserDelete(t *testing.T) {
	db, adminID, normalID := setupMembershipTest(t)
	ctx := context.Background()

	_, _ = db.GrantProjectAccess(ctx, normalID, "/proj", "viewer", adminID)

	// 删除用户应该 CASCADE 删除 membership
	_ = db.DeleteUser(ctx, normalID)

	projs, _ := db.ListUserProjects(ctx, normalID)
	if len(projs) != 0 {
		t.Errorf("expected 0 projects after user delete, got %d", len(projs))
	}
	_ = adminID // 防止 unused 警告
}
