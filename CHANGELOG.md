# Changelog

All notable changes to novel2all-go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-09-16

### Added (P0 Skeleton)
- Go module + `cmd/server` HTTP entry
- 健康检查端点 `GET /health` 返回 `{"status":"ok"}`
- 配置加载器 `internal/config`（环境变量 + `.env` 文件）
- 优雅关闭（SIGINT/SIGTERM → 等待 in-flight 请求 30s）
- GitHub Actions CI（build + vet + test）
- 完整文档：README / architecture / migration-mapping

### Notes
- P1 核心（llm/router + skills + chromem + graph）尚未实现
- 前端 dist 暂未挂载（P2 阶段）
- 生产部署尚未接入（P2 阶段）

[Unreleased]: https://github.com/ourvps1688/novel2all-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/ourvps1688/novel2all-go/releases/tag/v0.1.0