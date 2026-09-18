# novel2all-go

> 长篇小说自动化生产流水线 — **Go 全量重写 V2.0.0**（前身：[`ourvps1688/novel2all`](https://github.com/ourvps1688/novel2all) V1.5.5 Python）

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Status-V2.0.0_Vendor_Aligned-blue)](status)
[![CI](https://img.shields.io/badge/CI-passing-brightgreen?logo=github-actions)](actions)

## 这是什么

novel2all-go 是 novel2all Python 版的**全量 Go 重写**。目标是用 Go 单二进制 + 低内存占用 + 强类型 + 内置并发模型，替代 Python 3.12 + FastAPI + LiteLLM + ChromaDB + NetworkX 这一整套依赖。

13 个写作技能（SKILL.md prompt 模板 + 242 vendor references = 2.32MB）**直接复用**，零代码移植——通过 `embed.FS` 嵌入二进制。

**V2.0.0 vendor 对齐**（Sprint 37-43，2026-09 起）：**100% 对齐 vendor `oh-story-dsh-0.1.9`** — agent framework（RunAgent + Dispatcher + Orchestrator + DisallowedTools + PathTranslator）、7 个 vendor role、6 工具 + 沙箱、13 SKILL.md 完整版 + 242 references、3 中文 LLM（千问/DeepSeek/MiniMax-M3 国内版）走统一 anthropic 兼容协议。详见 [`docs/p3-vendor-alignment-plan.md`](docs/p3-vendor-alignment-plan.md)。

**V1.0.0 production-ready**：4 个 LLM provider + 5 层 memory + 8 节 short story pipeline + SSE 流式 + 23 个 starter references。

## 与前身对比

| 维度 | Python V1.5.5 | **Go V1.0.0（本项目）** |
|---|---|---|
| 运行时 | Python 3.12 + uvicorn | **单二进制 ~30MB** |
| 内存占用 | ~250MB | **~40MB** |
| 启动时间 | ~3s | **~50ms** |
| LLM router | LiteLLM (Python) | **自研 `internal/llm/router`** |
| 4 provider | dashscope/deepseek/MiniMax/anthropic | ✅ 全支持 (Sprint 21) |
| LLM tool call | Python V1 无 | ✅ Sprint 34 (OpenAI + Anthropic 格式) |
| 5 层 memory | Python V1 有 | ✅ Sprint 30 (Manager + Graph + Retriever + Tracker + Verifier + MultiAgentReviewer + Rollback + Summarizer + Extractor) |
| SKILL.md | 13 文件 Python loader | **embed.FS 嵌入二进制** |
| References | Python V1 无 references/ | ✅ Sprint 35 (23 starter refs + skill dispatch) |
| ProjectStructure | 13 methods | **15 methods (+ LoadAllSettings)** |
| Exporter | md/txt/epub/pdf | ✅ md/txt/epub/pdf + Chapter/BookMetadata + MaxBytes limits |
| Web SSE 流式 | Python V1 无 | ✅ Sprint 28 (5 endpoints + 2 task manager) |
| CLI 子命令 | Python V1 8 个 | ✅ Sprint 27 (version/status/setup/roles/skills/write/cache-migrate/web/memory) |
| 依赖管理 | uv + pyproject.toml | **Go modules（vendored 模式）** |
| 前端 | React 18 + Vite dist | **复用同一份 dist（无改动）** |

## V1.0.0 production-ready 核心能力

### Sprint 32-36 完整闭环
- **Sprint 32**：handleStream 真化（5 层 memory + 真 LLM + 真 outline + settings 自动加载）
- **Sprint 33**：13 个 SKILL.md 调整（对齐 Go 端实际能力，标注 status: full/partial/missing）
- **Sprint 34a**：LLM tool call（4 provider）+ MemoryGraph 4 tools（graph/foreshadow/timeline/character query）
- **Sprint 34b**：chapter_actions 加 5 层 memory 注入（流式抵消延迟）
- **Sprint 34c**：verifier post-check（5 action 全部支持 verifier Issues 字段）
- **Sprint 35**：references 库 + 23 个 starter reference + ProjectStructure.LoadAllSettings 遍历 设定/
- **Sprint 36**：E2E test 框架（mock_project + MockExecutor + 5 action + CompileShortStory + handleStream）+ scripts/e2e_real.sh

### 快速验证能力
```bash
# 1. 跑 mock E2E（不需要 LLM API key）
go test -count=1 -v ./internal/testfixtures/...

# 2. 跑真 LLM E2E（需要 API key）
./scripts/e2e_real.sh          # 默认：短篇 1 + 长篇 1
./scripts/e2e_real.sh short   # 只跑短篇
./scripts/e2e_real.sh long    # 只跑长篇
```

## 路线图

| 阶段 | 时长 | 状态 | 交付 |
|---|---|---|---|
| P0 骨架 | 2-3 周 | ✅ 完成 | go.mod + cmd/server + SQLite + .env + CI |
| P1 核心 | 8-10 周 | ✅ 完成 | llm/router + skills/loader + chromem-go + graph + exporter + auth |
| P1-E/M | 6-8 周 | ✅ 完成 | 4 provider + MemoryManager + ProjectStructure + Exporter |
| Sprint 16-26 | 8 周 | ✅ 完成 | CLI 8 子命令 + Memory 完整 + Short story pipeline + E2E 测试 |
| Sprint 27-31 | 4 周 | ✅ 完成 | Path C: CLI + SSE + ProjectStructure + Memory helpers + Exporter |
| Sprint 32-36 | 4 周 | ✅ 完成 | **V1.0.0 production-ready** (handleStream + tool call + verifier + references + E2E) |
| V1.0.x patches | 持续 | 🚧 | bug fix + 用户反馈 |
| V1.1 真 LLM 部署 | 2-4 周 | ⏳ | systemd unit + docker image + 灰度切换 |

详见 [`docs/architecture.md`](docs/architecture.md) 和 [`docs/p2-production-readiness-plan.md`](docs/p2-production-readiness-plan.md)。

## 快速开始

```bash
# 1. 克隆
git clone git@github.com:ourvps1688/novel2all-go.git
cd novel2all-go

# 2. 配置
cp configs/.env.example configs/.env
# 编辑 configs/.env，填入至少一个 LLM API key
#   DEEPSEEK_API_KEY / DASHSCOPE_API_KEY / MINIMAX_API_KEY / ANTHROPIC_API_KEY

# 3. 构建 + 运行
go build -o bin/novel2all ./cmd/server
./bin/novel2all

# 4. 验证
curl http://localhost:8000/health    # {"status":"ok"}
curl http://localhost:8000/version   # novel2all v1.0.0 ...

# 5. CLI 子命令
./bin/novel2all version
./bin/novel2all status --project-root /path/to/novel
./bin/novel2all setup --project-root /path/to/novel
./bin/novel2all roles
./bin/novel2all skills
./bin/novel2all cache-migrate --src backend.json --dst backend=sqlite
```

## 项目结构

```
novel2all-go/
├── cmd/
│   ├── server/          # HTTP server 入口
│   ├── cli/             # CLI 子命令 (version/status/setup/roles/skills/write/cache-migrate/web/memory)
│   └── migrate/         # 数据迁移工具
├── internal/
│   ├── api/             # HTTP handlers (28 files)
│   ├── auth/            # 鉴权 + Session + RateLimit + ProjectShare
│   ├── chroma/          # 向量检索 (纯 Go 实现)
│   ├── graph/           # 知识图谱
│   ├── llm/             # LLM router + 4 provider + cache + tool call
│   ├── memory/          # 5 层 memory 子系统 (9 files, 21 graph methods, 17 retriever methods)
│   ├── obs/             # metrics + tracing
│   ├── project/         # ProjectStructure (15 methods) + LoadAllSettings
│   ├── references/      # references 库 (23 starter + dispatch.go)
│   ├── roles/           # 5 角色 (story_outliner / chapter_writer / consistency_checker / story_reviewer / character_extractor)
│   ├── skills/          # 13 SKILL.md + Executor + Pipeline + CompileShortStory
│   ├── store/           # SQLite + cache + chapters + sessions + audit
│   ├── exporter/        # md/txt/epub/pdf 导出 + Chapter/BookMetadata + MaxBytes
│   ├── config/          # 配置加载
│   ├── testfixtures/    # Sprint 36 E2E fixtures (mock_project + MockExecutor)
│   └── server/          # 服务装配
├── docs/
│   ├── architecture.md
│   ├── p1-completion-plan.md
│   ├── p2-production-readiness-plan.md
│   └── ...
├── scripts/
│   ├── e2e_real.sh      # Sprint 36: 连真实 LLM 跑 E2E
│   └── ...
├── configs/
│   └── .env.example     # API keys 模板
├── .github/workflows/
│   └── ci.yml           # CI: lint + test + 6 build (4 OS × 2 Go) + smoke
├── .golangci.yml        # golangci-lint v1.61.0 配置
└── README.md
```

## 测试

```bash
# 全测试 (19 packages, 730+ PASS)
go test -count=1 -race ./...

# 单包
go test -count=1 -v ./internal/testfixtures/...   # E2E (mock)
go test -count=1 -v ./internal/memory/...         # 5 层 memory
go test -count=1 -v ./internal/references/...     # references 库
go test -count=1 -v ./internal/api/...            # HTTP handlers
```

## 文档

- [`docs/architecture.md`](docs/architecture.md) — 架构总览
- [`docs/p1-completion-plan.md`](docs/p1-completion-plan.md) — P1 阶段完成计划
- [`docs/p2-production-readiness-plan.md`](docs/p2-production-readiness-plan.md) — P2 阶段完成计划 (V1.0.0)

## 许可证

MIT — 详见 [LICENSE](LICENSE)

## 致谢

前身 [`ourvps1688/novel2all`](https://github.com/ourvps1688/novel2all) V1.5.5 Python 项目启发了本次 Go 全量重写。
