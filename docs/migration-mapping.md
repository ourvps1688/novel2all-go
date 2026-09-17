# Python → Go 迁移映射表

> 版本：v0.22.1 (P1 完成) · 最后更新：2026-09-17
> 前身：[`ourvps1688/novel2all`](https://github.com/ourvps1688/novel2all) V1.5.5 (Python 3.12)
> 公共仓库：`https://github.com/ourvps1688/novel2all-go`

## 0. 当前进度 (2026-09-17)

| 维度 | 数据 |
|---|---|
| 版本 | v0.22.1 |
| Go 源文件 | 87 |
| Go 测试文件 | 48 |
| 代码 LOC (含注释+空行) | ~23.4k |
| 单元测试 | **401 PASS / 0 FAIL** |
| 测试覆盖率 | ~13 包 |
| 嵌入 SKILL.md | 13 (embed.FS) |
| CI | 9/9 jobs pass (Sprint 18 + 19 hotfix) |

### 阶段进度

| 阶段 | 计划 | 实际 | 状态 |
|------|------|------|------|
| P0 骨架 | 2-3 周 | ~3 周 | ✅ |
| P1-A llm/router | 3 周 | ~3 周 | ✅ |
| P1-B skills/pipeline | 1.5 周 | ~1 周 | ✅ |
| P1-C chroma/graph | 1 周 | ~1 周 | ✅ |
| P1-D exporter | 1 周 | ~1 周 | ✅ |
| P1-E auth + store | 1.5 周 | ~1.5 周 | ✅ |
| P1-F API handlers | 2-3 周 | ~4 周 (12 切片) | ✅ |
| core/roles | (新增) | ~0.5 周 | ✅ |
| cmd/cli | (新增) | ~0.5 周 | ✅ |
| cmd/migrate | (新增) | ~0.5 周 | ✅ |
| scripts/ci_status.go | (新增) | ~0.5 周 | ✅ |

**P1 阶段 100% 完成**（见 `docs/p1-completion-plan.md` 6 sprint 全部 done）

## 1. 总览

| Python 文件/模块 | 行数 | Go 替代 | 工作量 | 状态 |
|---|---|---|---|---|
| `core/provider.py` | 1200 | `internal/llm/router.go` + 4 个 provider.go | 3 周 | ✅ |
| `core/cache.py` | 980 | `internal/store/cache.go` + `internal/llm/cache.go` | 1.5 周 | ✅ |
| `core/pipeline.py` | 370 | `internal/skills/pipeline.go` | 0.5 周 | ✅ |
| `core/exporter.py` | 540 | `internal/exporter/*.go` (gofpdf + go-epub) | 1 周 | ✅ |
| `core/embeddings.py` | 280 | `internal/chroma/client.go` (chromem-go) | 0.5 周 | ✅ |
| `core/graph.py` | 220 | `internal/graph/*.go` (BFS/DFS/Dijkstra) | 0.5 周 | ✅ |
| `core/instructor_patch.py` | 180 | `internal/llm/instructor.go` (instructor-go) | 0.3 周 | ✅ |
| 其他 core/* | 7000+ | 散落到 internal/ 各模块 | 2-3 周 | ✅ |
| `web/app.py` | 14005 | `cmd/server/main.go` + `internal/api/*.go` | 4-5 周 | ✅ |
| `cli/main.py` + sub-apps | 1200 | `cmd/cli/main.go` (std flag, no cobra) | 0.5 周 | ✅ |
| **13 个 SKILL.md** | 0 Python | **`internal/skills/assets/*.md`（embed.FS）** | **0 周** ✨ | ✅ |
| **总计** | **24722** | - | **18 周 (实际)** | **100%** |

## 2. 1:1 文件映射

### 2.1 Core 模块

| Python | Go | 说明 |
|---|---|---|
| `core/provider.py` | `internal/llm/router.go`<br>`internal/llm/dashscope.go`<br>`internal/llm/deepseek.go`<br>`internal/llm/minimax.go`<br>`internal/llm/anthropic.go` | 4-provider router + 各自 provider 实现 |
| `core/cache.py` | `internal/store/cache.go`<br>`internal/llm/cache.go` | SQLite LLM cache + 内存 prefix cache |
| `core/embeddings.py` | `internal/chroma/client.go` | chromem-go 替代 chromadb |
| `core/graph.py` | `internal/graph/graph.go`<br>`internal/graph/bfs.go`<br>`internal/graph/dijkstra.go` | 自研图算法 |
| `core/exporter.py` | `internal/exporter/md.go`<br>`internal/exporter/txt.go`<br>`internal/exporter/epub.go` | EPUB 用 `github.com/go-shogo/go-epub`<br>PDF 用 `github.com/jung-kurt/gofpdf` |
| `core/pipeline.py` | `internal/skills/pipeline.go` | stage runner |
| `core/instructor_patch.py` | `internal/llm/instructor.go` | instructor-go 适配 |
| `core/roles.py` | `internal/roles/*.go` | 5 个角色定义 |
| `core/short_mode.py` | `internal/skills/short_mode.go` | 短篇 8 节 |
| `core/deslop.py` | `internal/skills/deslop.go` | 去 AI 味 |

### 2.2 Web 模块

| Python | Go | 说明 |
|---|---|---|
| `web/app.py` | `cmd/server/main.go` + `internal/api/router.go` | 入口 + 路由表 |
| `web/auth.py` (内嵌) | `internal/api/auth.go` | login/me/logout/refresh |
| `web/users.py` (内嵌) | `internal/api/users.go` | 用户 CRUD |
| `web/projects.py` (内嵌) | `internal/api/projects.go` | 项目 CRUD |
| `web/chapters.py` (内嵌) | `internal/api/chapters.go` | 章节 CRUD |
| `web/write.py` (内嵌) | `internal/api/write.go` | SSE 流式生成 |
| `web/cache.py` (内嵌) | `internal/api/cache.go` | 缓存统计 |
| `web/skills.py` (内嵌) | `internal/api/skills.go` | 技能查询 |
| `web/roles.py` (内嵌) | `internal/api/roles.go` | 角色查询 |
| `web/admin.py` (内嵌) | `internal/api/admin.go` | admin 路由 |
| `web/audit.py` (内嵌) | `internal/api/audit.go` | 审计日志 |
| `web/metrics.py` (内嵌) | `internal/api/metrics.go` | Prometheus |
| `web/static_files.py` (内嵌) | `internal/api/static.go` | SPA mount + fallback |

### 2.3 CLI 模块

| Python | `cmd/cli/main.go` 子命令 | 说明 |
|---|---|---|
| `cli/main.py:skills` | `cmd/cli/skills.go` | 技能查询 |
| `cli/main.py:roles` | `cmd/cli/roles.go` | 角色查询 |
| `cli/main.py:write` | `cmd/cli/write.go` | 单次生成 |
| `cli/main.py:cache_migrate` | `cmd/cli/cache_migrate.go` | 缓存迁移 |
| `cli/main.py:web` | `cmd/cli/web.go` | 启动 web（等同 `cmd/server`） |

### 2.4 Skills（13 个 SKILL.md）— **0 行 Python 代码迁移**

```
novel2all/src/novel2all/skills/
├── STORY_GENRE_PLANNER.md       → internal/skills/assets/STORY_GENRE_PLANNER.md
├── CHAPTER_OUTLINE_GENERATOR.md → internal/skills/assets/CHAPTER_OUTLINE_GENERATOR.md
├── CHAPTER_WRITER.md            → internal/skills/assets/CHAPTER_WRITER.md
├── CHAPTER_REVIEWER.md          → internal/skills/assets/CHAPTER_REVIEWER.md
├── CONSISTENCY_CHECKER.md       → internal/skills/assets/CONSISTENCY_CHECKER.md
├── CHARACTER_EXTRACTOR.md       → internal/skills/assets/CHARACTER_EXTRACTOR.md
├── PLOT_SUMMARIZER.md           → internal/skills/assets/PLOT_SUMMARIZER.md
├── STORY_COVER.md               → internal/skills/assets/STORY_COVER.md
├── STORY_DESLOP.md              → internal/skills/assets/STORY_DESLOP.md
├── ... (其余 4 个 SKILL.md)    → 同上
```

**嵌入方式**：
```go
//go:embed assets/*.md
var skillFS embed.FS

func LoadAll() ([]Skill, error) {
    entries, _ := skillFS.ReadDir("assets")
    for _, e := range entries {
        data, _ := skillFS.ReadFile("assets/" + e.Name())
        // parse YAML frontmatter + body
    }
}
```

## 3. 数据库 Schema 迁移

Python 用 SQLite（单文件 `auth.db`），Go 也用 SQLite（同一文件格式，但表结构需重新设计）。

**P0 阶段只保留最简表**（auth.db）：
- `users` (id, username, password_hash, role, disabled, created_at)
- `sessions` (token, user_id, expires_at, ip)
- `audit_log` (id, event_type, user_id, username, ip, success, detail, timestamp)

**P1 阶段增加**：
- `projects` (id, name, owner_id, created_at)
- `chapters` (id, project_id, n, title, content, created_at, updated_at)
- `chapter_reviews` (chapter_id, agent, verdict, issues_json, created_at)
- `llm_cache` (key, prompt_hash, response, model, created_at)
- `adaptive_routing` (id, task, model, success_rate, updated_at)

**迁移脚本**：`cmd/migrate/main.go` —— P0 → P1 一次性从 Python auth.db 导入数据。

## 4. 配置文件迁移

Python `pyproject.toml` → Go `go.mod`（依赖声明）

Python `.env` → Go `configs/.env`（**字段名保持一致**，LLM keys 直接复用）

Python `configs/skills/*.yaml` → Go `internal/skills/assets/*.md`（同一份文件）

Python `configs/roles/*.yaml` → Go `internal/roles/*.go`（常量定义，编译时嵌入）

## 5. 测试迁移

| Python 测试 | Go 测试 | 备注 |
|---|---|---|
| `tests/unit/test_provider.py` | `internal/llm/router_test.go` | mock 4 providers |
| `tests/unit/test_cache.py` | `internal/store/cache_test.go` | |
| `tests/e2e/test_write_stream.py` | `internal/api/write_test.go` | httptest + SSE |
| `tests/e2e/test_static_assets.py` | `internal/api/static_test.go` | |
| `tests/test_ci_status.py` | `scripts/ci_status.go`（不放在 internal） | 调 GHCR_TOKEN |

## 6. CI 迁移

Python `.github/workflows/ci.yml` (test + lint + typecheck + build) → Go `.github/workflows/ci.yml`（精简版：build + vet + test）。

## 7. 部署迁移

Python `systemd/novel2all.service` → Go `deploy/systemd/novel2all-go.service`（**改用单二进制路径**）。

prod-deploy skill 改造 → `prod-deploy-go` skill（GitHub REST API 拉新 release + SSH 部署）。

## 8. 前端

**不动**。React 18 + Vite dist 直接复用，仅改 API client base URL（指向 `:8000` Go server）。

## 9. 时间线

| 阶段 | 周 | 累计 | 状态 | 完成日期 |
|------|---|------|------|---------|
| P0 骨架 | 2-3 | 2-3 | ✅ | 2026-09-16 |
| P1-A llm/router | 3 | 5-6 | ✅ | 2026-09-17 (Sprint 14) |
| P1-B skills/pipeline | 1.5 | 6.5-7.5 | ✅ | 2026-09-17 (Sprint 16) |
| P1-C chroma/graph | 1 | 7.5-8.5 | ✅ | 2026-09-17 (Sprint 13) |
| P1-D exporter | 1 | 8.5-9.5 | ✅ | 2026-09-17 (Sprint 13) |
| P1-E auth + store | 1.5 | 10-11 | ✅ | 2026-09-17 (Sprint 15) |
| P1-F API handlers | 2-3 | 12-14 | ✅ | 2026-09-17 (Sprint 12) |
| core/roles | 0.5 | 14.5 | ✅ | 2026-09-17 (Sprint 16) |
| cmd/cli + migrate | 1 | 15.5 | ✅ | 2026-09-17 (Sprint 18-19) |
| scripts/ci_status.go | 0.5 | 16 | ✅ | 2026-09-17 (Sprint 19) |
| P2-A 前端接入 | 2 | 18 | ⏳ | TBD |
| P2-B 部署 + 监控 | 2 | 20 | ⏳ | TBD |
| P2-C 灰度切换 | 2-4 | 22-24 | ⏳ | TBD |
| **P1 累计** | **~16 周** | **16** | **✅ 100%** | **2026-09-17** |
| **总计 (P2 后)** | | **22-24 周 (5.5-6 月)** | | |

> P1 比预估的 12-15 周晚了约 1-4 周, 主要因为:
> - P1-F 切片扩展到 12 个 (原本估 8-10 个)
> - Sprint 14 误判 "100% 完成" 后做严格盘点, 发现 19 个缺失项, 启动 6 sprint 补全
> - Sprint 17/19 各有一次 lint hotfix (goconst + gocyclo + bodyclose)

## 10. 下一阶段 (P2) 建议

| 优先级 | 项目 | 说明 |
|--------|------|------|
| 🔴 高 | P2-A 前端接入 | web-react/ Vite dist 部署到 novel2all-go (StaticHandler 已就绪) |
| 🟡 中 | P2-B 部署 + 监控 | prod-deploy-go skill + systemd unit + GHCR image |
| 🟢 低 | P2-C 灰度切换 | Python V1 + Go V2 并行, 流量切 10% → 50% → 100% |

### P2-A 启动条件

- ✅ StaticHandler 已实现 (Sprint 17 `internal/api/static.go`)
- ✅ Router `/` 路径兜底返回 index.html
- ⏳ 需要前端 Vite build → `web/dist/`
- ⏳ 需要配置 `STATIC_DIR=web/dist` 环境变量
- ⏳ 需要 `cmd/cli/web` 默认启用 static serving (目前 server.Run 没传)

### P2-B 启动条件

- ✅ `internal/server/server.go` 已抽出 (Sprint 18)
- ✅ `cmd/cli/web` 子命令可用
- ⏳ 需要 `prod-deploy-go` skill (监听 GitHub release → SSH 部署)
- ⏳ 需要 systemd unit file (`deploy/systemd/novel2all-go.service`)

### P2-C 启动条件

- ⏳ Python V1 + Go V2 同时运行 (不同端口)
- ⏳ 共享 SQLite (V1 写 + V2 读, 或反之) - 数据迁移已实现 (Sprint 19 `cmd/migrate`)
- ⏳ 灰度切流 (按 IP 或按用户 hash)

> 这是单人/小团队估时。如配齐 1 资深 + 2 中级 Go 开发者，可压缩到 3.5-4.5 月。