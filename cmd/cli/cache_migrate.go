// cache-migrate 子命令: cache 迁移工具.
//
// 当前状态: STUB — Sprint 15 完成后 cache 已默认用 SQLite L2, 新部署不需要迁移.
// 此子命令保留供未来从 in-memory cache JSON 迁到 SQLite llm_cache 表用.
//
// 用法:
//
//	novel2all cache-migrate --from=data/state.json --to=data/novel2all.db --dry-run
//
// flags:
//   - --config   .env 路径
//   - --from     源 JSON 路径 (默认 data/state.json)
//   - --to       目标 SQLite DSN (默认 data/novel2all.db)
//   - --dry-run  只打印计划, 不写入
package main

import (
	"flag"
	"fmt"
	"io"
)

// runCacheMigrate 执行 cache-migrate 子命令.
func runCacheMigrate(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("cache-migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	configPath := fs.String("config", "configs/.env", "path to .env file")
	from := fs.String("from", "data/state.json", "source JSON state file path")
	to := fs.String("to", "data/novel2all.db", "target SQLite DSN")
	dryRun := fs.Bool("dry-run", false, "print plan only, do not write")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cache-migrate: %w", err)
	}

	fmt.Fprintf(stderr, "cache-migrate (Sprint 18 stub)\n")
	fmt.Fprintf(stderr, "  config:  %s\n", *configPath)
	fmt.Fprintf(stderr, "  from:    %s\n", *from)
	fmt.Fprintf(stderr, "  to:      %s\n", *to)
	fmt.Fprintf(stderr, "  dry-run: %v\n", *dryRun)
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "当前状态 (Sprint 18, 2026-09-17):")
	fmt.Fprintln(stderr, "  - LLM cache 默认使用 SQLite L2 (llm_cache 表) + L1 内存")
	fmt.Fprintln(stderr, "  - 新部署不需要迁移 (直接用 schema003 即可)")
	fmt.Fprintln(stderr, "  - 此子命令保留为未来 P2 工具 (例如 V1 → V2 升级)")
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "未执行任何操作. 退出 0 (无错误).")

	return nil
}
