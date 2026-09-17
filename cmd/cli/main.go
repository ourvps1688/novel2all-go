// Command novel2all CLI 工具入口.
//
// 用法:
//
//	novel2all <subcommand> [flags]
//
// 子命令:
//
//	skills         列出所有 SKILL.md
//	roles          列出 5 个核心角色
//	write          单次写作生成 (调 LLM)
//	cache-migrate  cache 迁移 (stub, 见 cache_migrate.go)
//	web            启动 HTTP server (等同 cmd/server)
//
// 设计目的:
//   - 复用 internal/{skills,roles,llm,store} 等包, 无业务逻辑
//   - std flag 而非 cobra (避免 go.mod 升级)
//   - 每个子命令独立 Go 文件, 失败单独修复
//   - run() 接受 stdout/stderr 参数, 便于测试注入 buffer
package main

import (
	"fmt"
	"io"
	"os"
)

const (
	exitOK   = 0
	exitFail = 1
)

func main() {
	if err := run(os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "novel2all: %v\n", err)
		os.Exit(exitFail)
	}
}

// run 解析 subcommand + 路由分发.
//
// 不使用 cobra/spf13 (避免引入新依赖, Go 1.22 toolchain 锁).
// stdout/stderr 参数便于测试注入 buffer (main_test.go).
func run(stdout, stderr io.Writer, args []string) error {
	if len(args) == 0 {
		printHelp(stdout)
		return nil
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "skills":
		return runSkills(stdout, rest)
	case "roles":
		return runRoles(stdout, rest)
	case "write":
		return runWrite(stdout, stderr, rest)
	case "cache-migrate", "cache_migrate":
		return runCacheMigrate(stdout, stderr, rest)
	case "web":
		return runWeb(rest)
	case "-h", "--help", "help":
		printHelp(stdout)
		return nil
	default:
		fmt.Fprintf(stderr, "unknown subcommand: %q\n\n", cmd)
		printHelp(stderr)
		return fmt.Errorf("unknown subcommand: %q", cmd)
	}
}

// printHelp 输出顶层帮助到 w.
func printHelp(w io.Writer) {
	fmt.Fprintf(w, `novel2all CLI — novel2all-go 工具入口

用法:
  novel2all <subcommand> [flags]

子命令:
  skills         列出所有 SKILL.md (13 个 prompt 模板)
  roles          列出 5 个核心角色 (story_outliner/chapter_writer/...)
  write          单次写作生成 (调 LLM router, 输出到 stdout)
  cache-migrate  cache 迁移工具 (P1 stub, 当前版本不需要)
  web            启动 HTTP server (等同 cmd/server)

通用 flag:
  -h, --help     显示帮助

示例:
  novel2all skills
  novel2all roles --format=json
  novel2all write --task=WRITING --prompt "写一个侦探开场"
  novel2all web --port 8000
`)
}
