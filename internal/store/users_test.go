package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreateUser(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	id, err := db.CreateUser(ctx, "alice", "$2a$10$fakehash", "user")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id <= 0 {
		t.Errorf("id 应为正数，实际=%d", id)
	}

	// 重复用户名应报错
	_, err = db.CreateUser(ctx, "alice", "$2a$10$fakehash2", "user")
	if !errors.Is(err, ErrUserExists) {
		t.Errorf("重复用户应返回 ErrUserExists，实际=%v", err)
	}
}

func TestGetUserByUsername(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.CreateUser(ctx, "bob", "hash", "user")
	if err != nil {
		t.Fatal(err)
	}

	u, err := db.GetUserByUsername(ctx, "bob")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if u.Username != "bob" {
		t.Errorf("username 应为 bob，实际=%q", u.Username)
	}
	if u.Disabled {
		t.Error("新建用户不应 disabled")
	}
	if u.Role != "user" {
		t.Errorf("role 应为 user，实际=%q", u.Role)
	}

	// 不存在的用户
	_, err = db.GetUserByUsername(ctx, "nobody")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("不存在用户应返回 ErrUserNotFound，实际=%v", err)
	}
}

func TestSetUserDisabled(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	id, _ := db.CreateUser(ctx, "carol", "hash", "user")

	if err := db.SetUserDisabled(ctx, id, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	u, _ := db.GetUserByID(ctx, id)
	if !u.Disabled {
		t.Error("应被禁用")
	}

	if err := db.SetUserDisabled(ctx, id, false); err != nil {
		t.Fatalf("enable: %v", err)
	}
	u, _ = db.GetUserByID(ctx, id)
	if u.Disabled {
		t.Error("应被启用")
	}
}

func TestCountAdmins(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	if n, _ := db.CountAdmins(ctx); n != 0 {
		t.Errorf("初始应为 0，实际=%d", n)
	}
	db.CreateUser(ctx, "admin1", "h", "admin")
	db.CreateUser(ctx, "admin2", "h", "admin")
	db.CreateUser(ctx, "user1", "h", "user")
	if n, _ := db.CountAdmins(ctx); n != 2 {
		t.Errorf("应为 2，实际=%d", n)
	}
}
