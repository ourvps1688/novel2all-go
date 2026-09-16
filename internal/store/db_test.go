package store

import (
	"context"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpen(t *testing.T) {
	db := setupTestDB(t)
	// 验证连接
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestMigrate(t *testing.T) {
	db := setupTestDB(t)
	// 3 张表必须存在
	for _, table := range []string{"users", "sessions", "audit_log"} {
		var name string
		err := db.QueryRowContext(context.Background(),
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("表 %s 不存在: %v", table, err)
		}
	}
}
