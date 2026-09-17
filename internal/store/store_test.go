package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// openTestDB 创建临时内存 SQLite DB（自动建表）
func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")
	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// TestProjectsStore_CRUD projects 表 CRUD
func TestProjectsStore_CRUD(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewProjectsStore(db)

	// Create a user to satisfy FK
	_, _ = db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "alice", "x")

	// Create
	p, err := store.Create(ctx, "Alpha", "alpha", "First project", "fantasy", 1)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID == 0 {
		t.Error("ID should be assigned")
	}
	if p.Name != "Alpha" {
		t.Errorf("Name = %q", p.Name)
	}

	// Get
	p2, err := store.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p2.Slug != "alpha" {
		t.Errorf("Slug = %q", p2.Slug)
	}

	// GetBySlug
	p3, err := store.GetBySlug(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if p3.ID != p.ID {
		t.Errorf("ID mismatch")
	}

	// List
	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List count = %d", len(list))
	}

	// Update
	p4, err := store.Update(ctx, p.ID, "Alpha2", "Updated desc", "scifi")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p4.Name != "Alpha2" {
		t.Errorf("Name = %q", p4.Name)
	}

	// Delete
	if err := store.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, p.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: expected ErrNotFound, got %v", err)
	}
}

// TestProjectsStore_DuplicateSlug 项目 slug 唯一约束
func TestProjectsStore_DuplicateSlug(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewProjectsStore(db)
	_, _ = db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "u", "x")

	if _, err := store.Create(ctx, "P1", "shared", "", "", 1); err != nil {
		t.Fatalf("Create P1: %v", err)
	}
	if _, err := store.Create(ctx, "P2", "shared", "", "", 1); err == nil {
		t.Error("expected duplicate slug error")
	}
}

// TestProjectsStore_ListByOwner 按 owner 过滤
func TestProjectsStore_ListByOwner(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewProjectsStore(db)

	// 先插入一个 owner user（避免 FK 失败）
	_, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, "alice", "x")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := store.Create(ctx, "P1", "p1", "", "", 1); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Create(ctx, "P2", "p2", "", "", 1); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Create(ctx, "P3", "p3", "", "", 0); err == nil {
		t.Errorf("expected FK error for owner_id 0")
	}

	list, err := store.ListByOwner(ctx, 1)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 projects for owner 1, got %d", len(list))
	}
}

// TestChaptersStore_Upsert chapters upsert（按 project_id + n 唯一）
func TestChaptersStore_Upsert(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	projects := NewProjectsStore(db)
	chapters := NewChaptersStore(db)

	_, _ = db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "u", "x")
	p, err := projects.Create(ctx, "P", "p", "", "", 1)
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}

	// First upsert
	c1, err := chapters.Upsert(ctx, p.ID, 1, "Chapter One", "data/prose/第001章.md", 1000)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if c1.Title != "Chapter One" {
		t.Errorf("Title = %q", c1.Title)
	}

	// Second upsert (same project_id + n) → update
	c2, err := chapters.Upsert(ctx, p.ID, 1, "Chapter One v2", "data/prose/第001章.md", 1500)
	if err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}
	if c2.Title != "Chapter One v2" {
		t.Errorf("Title update = %q", c2.Title)
	}
	if c2.CharCount != 1500 {
		t.Errorf("CharCount = %d", c2.CharCount)
	}
	if c2.ID != c1.ID {
		t.Errorf("ID changed (should be same)")
	}

	// List
	list, err := chapters.List(ctx, p.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List count = %d", len(list))
	}
}

// TestChaptersStore_CascadeDelete 项目删除时 chapters 一起删
func TestChaptersStore_CascadeDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	projects := NewProjectsStore(db)
	chapters := NewChaptersStore(db)

	_, _ = db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "u", "x")
	p, _ := projects.Create(ctx, "P", "p", "", "", 1)
	for i := 1; i <= 3; i++ {
		if _, err := chapters.Upsert(ctx, p.ID, i, "北1", "", 100); err != nil {
			t.Fatalf("Upsert %d: %v", i, err)
		}
	}
	n, _ := chapters.CountByProject(ctx, p.ID)
	if n != 3 {
		t.Errorf("Count before delete = %d", n)
	}

	if err := projects.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete project: %v", err)
	}
	n, _ = chapters.CountByProject(ctx, p.ID)
	if n != 0 {
		t.Errorf("Count after delete = %d (expected 0)", n)
	}
}

// TestCacheStore_UpsertIncrement llm_cache upsert + hit_count++
func TestCacheStore_UpsertIncrement(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewCacheStore(db)

	if err := store.Upsert(ctx, "key1", "hash1", `{"text":"response1"}`, "model1"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Get
	e, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e.HitCount != 1 {
		t.Errorf("HitCount after first Upsert = %d (expected 1)", e.HitCount)
	}

	// IncrementHit
	if err := store.IncrementHit(ctx, "key1"); err != nil {
		t.Fatalf("IncrementHit: %v", err)
	}
	e, _ = store.Get(ctx, "key1")
	if e.HitCount != 2 {
		t.Errorf("HitCount after IncrementHit = %d (expected 2)", e.HitCount)
	}

	// Upsert same key
	if err := store.Upsert(ctx, "key1", "hash1", `{"text":"response2"}`, "model1"); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}
	e, _ = store.Get(ctx, "key1")
	if e.HitCount != 3 {
		t.Errorf("HitCount after Upsert = %d (expected 3)", e.HitCount)
	}
	if e.Response != `{"text":"response2"}` {
		t.Errorf("Response not updated")
	}
}

// TestCacheStore_Stats 统计
func TestCacheStore_Stats(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewCacheStore(db)

	for i := 0; i < 5; i++ {
		_ = store.Upsert(ctx, "key"+string(rune('a'+i)), "h", "r", "model-A")
	}
	_ = store.Upsert(ctx, "key-f", "h", "r", "model-B")

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalEntries != 6 {
		t.Errorf("TotalEntries = %d", stats.TotalEntries)
	}
	if stats.UniqueModels != 2 {
		t.Errorf("UniqueModels = %d", stats.UniqueModels)
	}
	if stats.TotalHits < 6 {
		t.Errorf("TotalHits = %d (expected >= 6)", stats.TotalHits)
	}
}

// TestCacheStore_PurgeOlderThan 删除过期
func TestCacheStore_PurgeOlderThan(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewCacheStore(db)

	if err := store.Upsert(ctx, "k1", "h", "r", "m"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// 强制把 last_hit_at 设为很久之前
	_, _ = db.ExecContext(ctx, `UPDATE llm_cache SET last_hit_at = '2020-01-01 00:00:00' WHERE key = 'k1'`)

	n, err := store.PurgeOlderThan(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("Purged %d (expected 1)", n)
	}
}

// TestReviewsStore_CRUD chapter_reviews CRUD
func TestReviewsStore_CRUD(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	projects := NewProjectsStore(db)
	chapters := NewChaptersStore(db)
	reviews := NewReviewsStore(db)

	_, _ = db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "u", "x")
	p, _ := projects.Create(ctx, "P", "p", "", "", 1)
	c, _ := chapters.Upsert(ctx, p.ID, 1, "T", "", 100)

	r, err := reviews.Create(ctx, c.ID, "consistency", "pass", `[]`)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if r.Agent != "consistency" {
		t.Errorf("Agent = %q", r.Agent)
	}

	list, err := reviews.ListByChapter(ctx, c.ID)
	if err != nil {
		t.Fatalf("ListByChapter: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("count = %d", len(list))
	}

	list2, err := reviews.ListByAgent(ctx, "consistency", 10)
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(list2) != 1 {
		t.Errorf("ListByAgent count = %d", len(list2))
	}
}

// TestRoutingStore_RecordResult adaptive_routing
func TestRoutingStore_RecordResult(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewRoutingStore(db)

	// Record 3 success, 1 fail → success_rate = 0.75
	for i := 0; i < 3; i++ {
		if err := store.RecordResult(ctx, "WRITING", "model-A", true); err != nil {
			t.Fatalf("RecordResult: %v", err)
		}
	}
	if err := store.RecordResult(ctx, "WRITING", "model-A", false); err != nil {
		t.Fatalf("RecordResult fail: %v", err)
	}

	r, err := store.Get(ctx, "WRITING", "model-A")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if r.TotalCount != 4 {
		t.Errorf("TotalCount = %d", r.TotalCount)
	}
	if r.SuccessCount != 3 {
		t.Errorf("SuccessCount = %d", r.SuccessCount)
	}
	if r.SuccessRate < 0.74 || r.SuccessRate > 0.76 {
		t.Errorf("SuccessRate = %f", r.SuccessRate)
	}
}

// TestRoutingStore_BestModelForTask 找最佳 model
func TestRoutingStore_BestModelForTask(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewRoutingStore(db)

	// model-A: 5/5 success (1.0)
	for i := 0; i < 5; i++ {
		_ = store.RecordResult(ctx, "WRITING", "model-A", true)
	}
	// model-B: 1/1 success (1.0) — sample too small for minSamples=2
	_ = store.RecordResult(ctx, "WRITING", "model-B", true)
	// model-C: 2/2 fail (0.0)
	for i := 0; i < 2; i++ {
		_ = store.RecordResult(ctx, "WRITING", "model-C", false)
	}

	best, err := store.BestModelForTask(ctx, "WRITING", 2)
	if err != nil {
		t.Fatalf("BestModelForTask: %v", err)
	}
	if best.Model != "model-A" {
		t.Errorf("best = %q (expected model-A)", best.Model)
	}
}

// TestRoutingStore_ListByTask 列出 task 所有 model
func TestRoutingStore_ListByTask(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewRoutingStore(db)

	_ = store.RecordResult(ctx, "WRITING", "model-A", true)
	_ = store.RecordResult(ctx, "WRITING", "model-B", true)
	_ = store.RecordResult(ctx, "EXTRACTION", "model-C", true)

	list, err := store.ListByTask(ctx, "WRITING")
	if err != nil {
		t.Fatalf("ListByTask: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("count = %d", len(list))
	}
}
