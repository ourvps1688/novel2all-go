# novel2all-go 架构设计

> 版本：v0.1.0 (P0 Skeleton) · 最后更新：2026-09-16

## 1. 总览

novel2all-go 采用**经典三层架构 + 自研 LLM router + embed.FS 嵌入 skills**：

```
┌─────────────────────────────────────────────────────────────┐
│  Client (React 18 SPA / curl / pytest)                      │
└──────────────┬──────────────────────────────────────────────┘
               │ HTTP/JSON
               ▼
┌─────────────────────────────────────────────────────────────┐
│  cmd/server (Go 1.22+)                                      │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ internal/api (Gin router)                           │    │
│  │   /health /api/auth/* /api/projects/* /api/write/* │    │
│  └──────┬──────────┬──────────────┬──────────────────┘    │
│         ▼          ▼              ▼                        │
│  ┌──────────┐ ┌──────────┐  ┌──────────────────┐          │
│  │ auth/    │ │ skills/  │  │ llm/router       │          │
│  │ Session  │ │ embed.FS │  │ (自研 4-prov)    │          │
│  │ + RBAC   │ │ SKILL.md │  │ dashscope/deepseek│          │
│  └────┬─────┘ └────┬─────┘  │ minimax/anthropic │          │
│       │            │        └─────┬────────────┘          │
│       ▼            ▼              ▼                        │
│  ┌──────────────────────────────────────┐                 │
│  │ store/ (SQLite + BoltDB)              │                 │
│  └──────────────────────────────────────┘                 │
└─────────────────────────────────────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────────────────────┐
│  External: LLM APIs / ChromaDB / Browser (chromedp)         │
└─────────────────────────────────────────────────────────────┘
```

## 2. 目录与职责

```
novel2all-go/
├── cmd/server/main.go            # 入口：装配 + 启停 + 信号处理
├── internal/
│   ├── api/      # Gin router + handlers
│   ├── auth/     # Session Cookie + bcrypt + RBAC
│   ├── config/   # .env 加载 + 校验
│   ├── llm/      # router: 4-provider 透明分流
│   ├── skills/   # embed.FS 加载 SKILL.md + prompt 渲染
│   ├── chroma/   # chromem-go 封装（向量检索）
│   ├── graph/    # 自研图算法（替代 NetworkX）
│   ├── exporter/ # PDF/EPUB/DOCX 导出
│   ├── store/    # SQLite (sqlx) + BoltDB (kv)
│   └── obs/      # prometheus metrics + 结构化日志
├── web/ # React 18 dist（从 novel2all 复用，P2 阶段）
├── configs/.env.example
├── deploy/systemd/novel2all-go.service
├── scripts/ci.sh
├── .github/workflows/ci.yml
├── docs/
└── go.mod
```

## 3. 关键模块设计

### 3.1 LLM Router (`internal/llm`)

**职责**：根据 `task type`（WRITING/CONSISTENCY/EXTRACTION/SUMMARIZATION/COVER）自动选择 provider + 模型。

```go
type TaskType string
const (
  TaskWriting       TaskType = "WRITING"
  TaskConsistency   TaskType = "CONSISTENCY"
  TaskExtraction    TaskType = "EXTRACTION"
  TaskSummarization TaskType = "SUMMARIZATION"
  TaskCover         TaskType = "COVER"
)

type Router struct {
  providers map[string]Provider // dashscope / deepseek / minimax / anthropic
  routes    map[TaskType]Route  // task -> provider + model
}

func (r *Router) Complete(ctx, task TaskType, prompt string, opts ...Option) (*Response, error)
```

**关键决策**（来自 Python V1.5.5 实测）：
- WRITING → minimax M3（质量最优，毫秒级响应）
- 其他 4 task → DeepSeek flash（结构化任务，便宜够用）

### 3.2 Skills Loader (`internal/skills`)

**核心思想**：13 个 SKILL.md 是**纯 prompt 模板**，0 行 Python 代码。通过 `embed.FS` 嵌入二进制。

```go
//go:embed assets/*.md
var skillFS embed.FS

type Skill struct {
  Name        string
  Description string
  Template    string // 原始 markdown，含 {{变量}}
}

func LoadAll() ([]Skill, error)
func (s *Skill) Render(ctx context.Context, vars map[string]any) (string, error)
```

调用流程：`LoadAll() → 选 skill → Render(项目变量) → 调 llm.Router.Complete() → 流式 SSE 输出`。

### 3.3 Store (`internal/store`)

- **SQLite**（via `mattn/go-sqlite3`）：用户、项目、章节、审计日志
- **BoltDB**（via `go.etcd.io/bbolt`）：缓存、会话、KV 数据

两个 store 各司其职，避免混合依赖。

### 3.4 Auth (`internal/auth`)

延续 Python 版设计：Session Cookie + 单账号 + 简单计数限流。

```go
// Session Cookie: HttpOnly + SameSite=Strict
// ADMIN_USER/ADMIN_PASS 通过环境变量注入
// 5 分钟内 5 次失败 → 锁定 5 分钟
```

### 3.5 Config (`internal/config`)

```go
type Config struct {
  HTTP      HTTPConfig
  Log       LogConfig
  DB        DBConfig
  LLM       LLMConfig
  Skills    SkillsConfig
  GitHub    GitHubConfig
}

func Load() (*Config, error)  // .env + env vars 覆盖
func (c *Config) Validate() error
```

## 4. HTTP API（45 endpoints，P1/P2 逐步实现）

### P0 必备（本次交付）
- `GET /health` → `{"status":"ok"}`
- `GET /version` → `{"version":"0.1.0","commit":"..."}`

### P1 必备
- `/api/auth/{login,me,logout,refresh}`
- `/api/projects*` / `/api/chapters*` / `/api/write/{stream,active,cancel}`
- `/api/skills*` / `/api/cache*` / `/api/roles`

### P2 必备
- 前端 SPA mount + fallback
- 审计日志 + admin 路由
- 文件上传 + 导出下载

## 5. 部署

### systemd（推荐）
```ini
[Unit]
Description=novel2all-go
After=network.target

[Service]
Type=simple
User=novel2all
EnvironmentFile=/etc/novel2all/novel2all-go.env
ExecStart=/opt/novel2all-go/bin/novel2all
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

### 手动（验证用）
```bash
./bin/novel2all  # 读 configs/.env
```

## 6. 监控

- Prometheus metrics 暴露 `/metrics`
- 结构化日志（slog + JSON）
- 关键指标：llm_request_duration_seconds, skill_render_duration_seconds, active_sessions, db_query_duration_seconds

## 7. 已知风险

| 风险 | 缓解 |
|---|---|
| chromedp 在 sandbox CI 跑不起来 | CI 跳过 browser-cdp 测试，本地手测 |
| minimax Anthropic 端点 404 | router 内部 dual-protocol，httpx 直连 |
| SQLite 并发写锁 | 用 WAL 模式 + connection pool |
| embed.FS 增加二进制大小 | 13 个 SKILL.md 总 < 200KB，可接受 |

---

> 下一步：见 `migration-mapping.md` — Python → Go 1:1 文件映射表。