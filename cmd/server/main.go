// Command novel2all-go 是 novel2all-go 的 HTTP 服务入口.
//
// 用法:
//
//	novel2all-go [--config configs/.env]
//
// 环境变量见 configs/.env.example.
//
// Sprint 18: 启动逻辑重构到 internal/server 包, 此文件仅作为 thin wrapper.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ourvps1688/novel2all-go/internal/config"
	"github.com/ourvps1688/novel2all-go/internal/server"
)

func main() {
	configPath := flag.String("config", "configs/.env", "path to .env file (use empty to skip)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: load config: %v\n", err)
		os.Exit(1)
	}

	if err := server.Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
