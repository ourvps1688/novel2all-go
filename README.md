# novel2all-go

> 长篇小说自动化生产流水线 — **Go 全量重写 V2**（前身：[`ourvps1688/novel2all`](https://github.com/ourvps1688/novel2all) V1.5.5 Python）

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Status-P0%20Skeleton-orange)](status)

## 这是什么

novel2all-go 是 novel2all Python 版的**全量 Go 重写**。目标是用 Go 单二进制 + 低内存占用 + 强类型 + 内置并发模型，替代 Python 3.12 + FastAPI + LiteLLM + ChromaDB + NetworkX 这一整套依赖。

13 个写作技能（SKILL.md prompt 模板）**直接复用**，零代码移植——通过 `embed.FS` 嵌入二进制。

## 与前身对比

| 维度 | Python V1.5.5 | **Go V2（本项目）** |
|---|---|---|
| 运行时 | Python 3.12 + uvicorn | **单二进制 ~30MB** |
| 内存占用 | ~250MB | **~40MB** |
| 启动时间 | ~3s | **~50ms** |
| LLM router | LiteLLM (Python) | **自研 `internal/llm/router`** |
| 依赖管理 | uv + pyproject.toml | **Go modules（vendored 模式）** |
| 13 skills | Python SKILL.md loader | **embed.FS 嵌入二进制** |
| 前端 | React 18 + Vite dist | **复用同一份 dist（无改动）** |

## 路线图

| 阶段 | 时长 | 状态 | 交付 |
|---|---|---|---|
| **P0 骨架** | 2-3 周 | 🚧 进行中 | go.mod + cmd/server + SQLite + .env + CI；`curl localhost:8000/health` 返回 200 |
| P1 核心 | 8-10 周 | ⏳ | llm/router + skills/loader + chromem-go + graph + exporter + auth |
| P2 前端 + 部署 | 4-6 周 | ⏳ | web-react 接入新 API + systemd + prod-deploy-go skill + 灰度切换 |

详见 [`docs/architecture.md`](docs/architecture.md)。

## 快速开始

```bash
# 1. 克隆
git clone git@github.com:ourvps1688/novel2all-go.git
cd novel2all-go

# 2. 配置
cp configs/.env.example configs/.env
# 编辑 configs/.env，填入 LLM API keys

# 3. 构建 + 运行
go build -o bin/novel2all ./cmd/server
./bin/novel2all

# 4. 健康检查
curl http://localhost:8000/health
```

## 项目结构

```
novel2all-go/
├── cmd/server/main.go            # HTTP 入口
├── internal/
│   ├── api/                       # HTTP handlers
│   ├── llm/                       # 4-provider router (自研)
│   ├── skills/                    # SKILL.md loader (embed.FS)
│   ├── chroma/                    # chromem-go 封装
│   ├── graph/                     # NetworkX 替代
│   ├── exporter/                  # PDF/EPUB/DOCX 导出
│   ├── auth/                      # Session + RBAC
│   └── store/                     # SQLite + BoltDB
├── web/ # React 18 + Vite dist (从 novel2all 复用)
├── configs/.env.example
├── deploy/systemd/novel2all-go.service
├── docs/
│   ├── architecture.md
│   └── migration-mapping.md
├── scripts/ci.sh
├── .github/workflows/ci.yml
├── go.mod
├── README.md
├── CHANGELOG.md
├── LICENSE└── .gitignore
```

## 历史

本项目前身 [novel2all (Python)](https://github.com/ourvps1688/novel2all) 已于 **2026-09-16 Archived，进入冻结保留状态**。

迁移映射表见 [`docs/migration-mapping.md`](docs/migration-mapping.md)。

## 贡献

过渡期内（4-5 个月）**只接受 P1/P2 阶段的功能 PR**，不接受 Python 版回炉改动。

## 许可证

[MIT](LICENSE) © 2026 ourvps1688