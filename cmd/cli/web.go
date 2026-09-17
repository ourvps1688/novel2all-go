// web 子命令: 启动 HTTP server (等同 cmd/server).
//
// 用法:
//
//	novel2all web [--config configs/.env] [--port 8000]
//
// flags:
//   - --config  .env 路径 (默认 configs/.env, 空字符串跳过)
//   - --port    HTTP 端口 (覆盖 cfg.HTTP.Port)
package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/ourvps1688/novel2all-go/internal/config"
	"github.com/ourvps1688/novel2all-go/internal/server"
)

// runWeb 执行 web 子命令.
//
// Sprint 18 重构: 启动逻辑在 internal/server.Run(cfg) 里, 此处只解析 CLI flags.
func runWeb(args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	configPath := fs.String("config", "configs/.env", "path to .env file (empty to skip)")
	port := fs.Int("port", 0, "HTTP port (0 = use cfg.HTTP.Port from config)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("web: %w", err)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if *port > 0 {
		cfg.HTTP.Port = *port
	}

	return server.Run(cfg)
}
