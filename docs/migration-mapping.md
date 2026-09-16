# Python → Go 迁移映射表

> 版本：v0.1.0 (P0 Skeleton) · 最后更新：2026-09-16
> 前身：[`ourvps1688/novel2all`](https://github.com/ourvps1688/novel2all) V1.5.5 (Python 3.12)

## 1. 总览

| Python 文件/模块 | 行数 | Go 替代 | 工作量 |
|---|---|---|---|
| `core/provider.py` | 1200 | `internal/llm/router.go` + 4 个 provider.go | 3 周 |
| `core/cache.py` | 980 | `internal/store/cache.go` + `internal/llm/cache.go` | 1.5 周 |
| `core/pipeline.py` | 370 | `internal/skills/pipeline.go` | 0.5 周 |
| `core/exporter.py` | 540 | `internal/exporter/*.go` (gofpdf + go-epub) | 1 周 |
| `core/embeddings.py` | 280 | `internal/chroma/client.go` (chromem-go) | 0.5 周 |
| `core/graph.py` | 220 | `internal/graph/*.go` (BFS/DFS/Dijkstra) | 0.5 周 |
| `core/instructor_patch.py` | 180 | `internal/llm/instructor.go` (instructor-go) | 0.3 周 |
| 其他 core/* | 7000+ | 散落到 internal/ 各模块 | 2-3 周 |
| `web/app.py` | 14005 | `cmd/server/main.go` + `internal/api/*.go` | 4-5 周 |
| `cli/main.py` + sub-apps | 1200 | `cmd/cli/main.go` + cobra | 0.5 周 |
| **13 个 SKILL.md** | 0 Python | **`internal/skills/assets/*.md`（embed.FS）** | **0 周** ✨ |
| **总计** | **24722** | - | **12-15 周** |

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

| 阶段 | 周 | 累计 | 状态 |
|---|---|---|---|
| P0 骨架 | 2-3 | 2-3 | 🚧 |
| P1-A llm/router | 3 | 5-6 | ⏳ |
| P1-B skills/pipeline | 1.5 | 6.5-7.5 | ⏳ |
| P1-C chroma/graph | 1 | 7.5-8.5 | ⏳ |
| P1-D exporter | 1 | 8.5-9.5 | ⏳ |
| P1-E auth + store | 1.5 | 10-11 | ⏳ |
| P1-F API handlers | 2-3 | 12-14 | ⏳ |
| P2-A 前端接入 | 2 | 14-16 | ⏳ |
| P2-B 部署 + 监控 | 2 | 16-18 | ⏳ |
| P2-C 灰度切换 | 2-4 | 18-22 | ⏳ |
| **总计** | | **18-22 周 (4.5-5.5 月)** | |

> 这是单人/小团队估时。如配齐 1 资深 + 2 中级 Go 开发者，可压缩到 3.5-4.5 月。