// version.go — Sprint 27 CLI 子命令: version
//
// novel2all version — 显示版本号 + commit + build time.
//
// 对齐 Python V1 cli/main.py version() 命令.
package main

import (
	"fmt"
	"io"

	"github.com/ourvps1688/novel2all-go/internal/version"
)

// runVersion 输出版本信息到 stdout.
func runVersion(stdout io.Writer, _ []string) error {
	info := version.Get()
	fmt.Fprintf(stdout, "novel2all v%s\n", info.Version)
	fmt.Fprintf(stdout, "  commit:     %s\n", info.Commit)
	fmt.Fprintf(stdout, "  built:      %s\n", info.BuildTime)
	fmt.Fprintf(stdout, "  go version: %s\n", info.GoVersion)
	return nil
}
