// cache-migrate 子命令: cache 迁移工具 (Sprint 27 真功能).
//
// novel2all cache-migrate --from=<json> --to=<sqlite> [--max-size=1024] [--ttl=0]
// 从 JSON cache 迁移到 SQLite cache (或反向).
//
// 用法:
//
//	novel2all cache-migrate --from=.chroma/cache.json --to=.chroma/cache.db
//	novel2all cache-migrate --from=.chroma/cache.db --to=.chroma/cache.json --src=sqlite --dst=json
//
// flags:
//
//	--src=json|sqlite    源 backend (auto-detect from .json/.db)
//	--dst=json|sqlite    目标 backend (auto-detect from .json/.db)
//	--from=PATH          源 cache 文件路径
//	--to=PATH            目标 cache 文件路径
//	--max-size=N         目标 max_size (默认 1024)
//	--ttl=N              目标 TTL 秒数 (默认 0 = 不过期)
//	--dry-run            只打印计划, 不写入 (V0 不支持, 总是真实迁移)
package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// runCacheMigrate 执行 cache-migrate 子命令.
func runCacheMigrate(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("cache-migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fromPath := fs.String("from", "", "source cache file path")
	toPath := fs.String("to", "", "target cache file path")
	srcBackend := fs.String("src", "auto", "source backend: json|sqlite|auto (auto-detect from extension)")
	dstBackend := fs.String("dst", "auto", "target backend: json|sqlite|auto")
	maxSize := fs.Int("max-size", 1024, "target max_size")
	ttlSeconds := fs.Int("ttl", 0, "target TTL in seconds (0 = never expire)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cache-migrate: %w", err)
	}

	if *fromPath == "" {
		return fmt.Errorf("--from is required")
	}
	if *toPath == "" {
		return fmt.Errorf("--to is required")
	}

	// auto-detect backend
	src := *srcBackend
	if src == "auto" {
		if strings.HasSuffix(*fromPath, ".json") {
			src = "json"
		} else if strings.HasSuffix(*fromPath, ".db") || strings.HasSuffix(*fromPath, ".sqlite") {
			src = "sqlite"
		} else {
			return fmt.Errorf("cannot auto-detect src backend from %q, use --src=json|sqlite", *fromPath)
		}
	}
	dst := *dstBackend
	if dst == "auto" {
		if strings.HasSuffix(*toPath, ".json") {
			dst = "json"
		} else if strings.HasSuffix(*toPath, ".db") || strings.HasSuffix(*toPath, ".sqlite") {
			dst = "sqlite"
		} else {
			return fmt.Errorf("cannot auto-detect dst backend from %q, use --dst=json|sqlite", *toPath)
		}
	}

	if src == dst && *fromPath == *toPath {
		return fmt.Errorf("source and target are identical")
	}

	if src == "memory" || dst == "memory" {
		return fmt.Errorf("memory backend not supported for migration")
	}

	fmt.Fprintf(stdout, "迁移 cache: %s → %s\n", src, dst)
	fmt.Fprintf(stdout, "  src: %s\n", *fromPath)
	fmt.Fprintf(stdout, "  dst: %s\n", *toPath)
	fmt.Fprintf(stdout, "  max_size=%d, ttl=%ds\n\n", *maxSize, *ttlSeconds)

	progress := func(done, total int) {
		fmt.Fprintf(stdout, "  进度: %d/%d\n", done, total)
	}

	result, err := store.MigrateCache(src, dst, *fromPath, *toPath,
		*maxSize, *ttlSeconds, progress)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, result.Summary())

	if len(result.Errors) > 0 {
		fmt.Fprintf(stdout, "\n[警告] %d 条迁移失败:\n", len(result.Errors))
		for _, e := range result.Errors {
			if len(e) > 100 {
				e = e[:100] + "..."
			}
			fmt.Fprintf(stdout, "  - %s\n", e)
		}
		return fmt.Errorf("migration completed with %d errors", len(result.Errors))
	}

	if result.Migrated > 0 {
		fmt.Fprintf(stdout, "\n[提示] 迁移成功! 建议备份或删除源文件 %s (手动)\n", *fromPath)
	}
	return nil
}
