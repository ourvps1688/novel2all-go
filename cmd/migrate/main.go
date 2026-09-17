// Command migrate 从 Python novel2all auth.db 迁移用户数据到 novel2all-go SQLite.
//
// 用法:
//
//	migrate --from=path/to/auth.db --to=data/novel2all.db [--dry-run] [--reset-admin]
//
// flags:
//   - --from         Python auth.db 路径 (必填)
//   - --to           Go SQLite DSN (默认 data/novel2all.db)
//   - --dry-run      只读源 db + 打印计划, 不写入目标
//   - --reset-admin  迁移后用 ADMIN_PASS 环境变量重置 admin 用户密码 (Python passlib pbkdf2 不兼容 Go bcrypt)
//
// 设计目的:
//   - V1 (Python novel2all) → V2 (Go novel2all-go) 用户数据迁移
//   - 当前只迁 users 表 (Python auth.db 通常只有这一张)
//   - password_hash 字段保留原 hash, 但 Python passlib 与 Go bcrypt 不兼容
//     所以默认禁用 admin, --reset-admin 用 bcrypt 重置
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	_ "modernc.org/sqlite" // 注册 sqlite driver

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

const (
	exitOK   = 0
	exitFail = 1
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(exitFail)
	}
}

// run 解析 flags + 执行迁移.
func run(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	from := fs.String("from", "", "Python auth.db path (required)")
	to := fs.String("to", "data/novel2all.db", "Go SQLite DSN")
	dryRun := fs.Bool("dry-run", false, "print plan only, do not write")
	resetAdmin := fs.Bool("reset-admin", false, "reset admin password using ADMIN_PASS env var")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("flag: %w", err)
	}

	if *from == "" {
		return fmt.Errorf("--from is required (Python auth.db path)")
	}

	// 1. 打开 Python auth.db (只读)
	fmt.Fprintf(os.Stderr, "[1/4] opening source %s ...\n", *from)
	srcDB, err := openSource(*from)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = srcDB.Close() }()

	// 2. 读取 users
	fmt.Fprintln(os.Stderr, "[2/4] reading users ...")
	users, err := readUsers(srcDB)
	if err != nil {
		return fmt.Errorf("read users: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  found %d users\n", len(users))
	for _, u := range users {
		fmt.Fprintf(os.Stderr, "    id=%d username=%s role=%s disabled=%v\n",
			u.ID, u.Username, u.Role, u.Disabled)
	}

	if *dryRun {
		fmt.Fprintln(os.Stderr, "[dry-run] 不打开目标 db, 不写入. exit 0")
		return nil
	}

	// 3. 打开 Go SQLite (调 store.Open + Migrate)
	fmt.Fprintf(os.Stderr, "[3/4] opening target %s ...\n", *to)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tgtDB, err := store.Open(ctx, *to)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer func() { _ = tgtDB.Close() }()

	if err := tgtDB.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate target: %w", err)
	}

	// 4. 批量插入 users
	fmt.Fprintln(os.Stderr, "[4/4] inserting users ...")
	for _, u := range users {
		// password_hash 不直接复制 (pbkdf2 → bcrypt 不兼容)
		// 用空 hash 占位 (登录会被拒) + 设置 disabled=1 强制重置
		// 除非 --reset-admin 且该用户是 admin
		hash := ""
		disabled := true // 默认禁用, 强制用户重置密码
		if *resetAdmin && u.Username == "admin" {
			newPass := os.Getenv("ADMIN_PASS")
			if newPass == "" {
				return fmt.Errorf("--reset-admin requires ADMIN_PASS env var")
			}
			h, err := auth.HashPassword(newPass)
			if err != nil {
				return fmt.Errorf("hash new admin password: %w", err)
			}
			hash = h
			disabled = false
			fmt.Fprintf(os.Stderr, "  admin password reset via ADMIN_PASS env var\n")
		}

		_, err := tgtDB.ExecContext(ctx,
			`INSERT INTO users (username, password_hash, role, disabled, created_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(username) DO NOTHING`,
			u.Username, hash, u.Role, disabled, time.Unix(int64(u.CreatedAt), 0).UTC())
		if err != nil {
			return fmt.Errorf("insert user %q: %w", u.Username, err)
		}
	}

	fmt.Fprintf(os.Stderr, "\nMigrate 完成. %d users migrated to %s\n", len(users), *to)
	fmt.Fprintln(os.Stderr, "注意: 用户的 Python pbkdf2 password hash 不兼容 Go bcrypt, 已默认禁用")
	if !*resetAdmin {
		fmt.Fprintln(os.Stderr, "      用 --reset-admin + ADMIN_PASS 环境变量重置 admin 密码")
	}
	return nil
}

// pyUser 兼容 Python auth.db 的 users 表 schema.
type pyUser struct {
	ID           int64
	Username     string
	PasswordHash string // 不使用, 仅占位
	Role         string
	CreatedAt    float64 // REAL (Unix timestamp)
	Disabled     bool
}

// openSource 打开 Python auth.db (只读).
func openSource(path string) (*sql.DB, error) {
	// 注意: Python 用 INTEGER PRIMARY KEY + REAL timestamp, Go 表用 INTEGER id + DATETIME created_at
	// schema 略不同但 modernc sqlite 兼容
	dsn := fmt.Sprintf("file:%s?mode=ro", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// readUsers 读 Python auth.db users 表.
//
// 列: id, username, password_hash, role, created_at (REAL), disabled (INTEGER 0/1).
func readUsers(db *sql.DB) ([]pyUser, error) {
	rows, err := db.Query(`SELECT id, username, password_hash, role, created_at, disabled FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []pyUser
	for rows.Next() {
		var u pyUser
		var disabled int
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &disabled); err != nil {
			return nil, err
		}
		u.Disabled = disabled != 0
		out = append(out, u)
	}
	return out, rows.Err()
}
