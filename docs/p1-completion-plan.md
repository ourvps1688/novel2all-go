# P1 阶段补全计划（2026-09-17）

> 严格盘点见对话历史。19 个缺失项已识别。
> 本文档按依赖 + 工作量分 6 个 sprint 逐步补全。

## 总览

| Sprint | 主题 | 缺失项 | 工作量 | 风险 |
|---|---|---|---|---|
| 15 | DB schema + LLM cache | 6 | ~3 周 | **高** |
| 16 | Core 残余（roles/short_mode/deslop wrappers） | 6 | ~1 周 | 低 |
| 17 | Web 残余（admin/static/命名整理） | 4 | ~1.5 周 | 中 |
| 18 | CLI 工具（5 子命令） | 6 | ~1 周 | 低 |
| 19 | Tools（migrate + ci_status + deploy skill） | 3 | ~1.5 周 | 中 |
| 20 | 收尾（static_test + 验证） | 0 | ~0.5 周 | 低 |
| **合计** | | **25** | **~8.5 周** | |

> 注：19 个缺失项目前按文件计 = 25 个子工作（CLI 5 文件展开 + 5 DB 表展开）

---

## Sprint 15：P1-DB 残余 + LLM cache（核心架构，**最大块**）

### 目标
补齐 5 个 P1 表（projects/chapters/chapter_reviews/llm_cache/adaptive_routing）+ 实现 LLM cache 双层（内存 prefix + SQLite 持久化）

### 文件清单（11 个）
- **新增** `internal/store/cache.go`（SQLite LLM cache storage layer）
- **新增** `internal/llm/cache.go`（内存 prefix cache + SQLite 集成）
- **新增** `internal/store/cache_test.go`（cache unit + benchmark）
- **修改** `internal/store/migration.go`（加 5 个 P1 表 schema + migrations）
- **修改** `internal/store/projects.go`（新增 `*SQLiteStore` 替代内存 ProjectStore）
- **修改** `internal/store/chapters.go`（新增 SQLite chapters 表 + filesystem 双写）
- **修改** `internal/store/reviews.go`（新增 SQLite chapter_reviews 表）
- **修改** `internal/store/routing.go`（新增 SQLite adaptive_routing 表）
- **修改** `internal/api/projects.go`（改用 SQLiteProjectStore）
- **修改** `internal/api/chapters.go`（chapters 持久化到 SQLite + filesystem 双写）
- **修改** `cmd/server/main.go`（注入新 store）

### 关键决策
1. **chapters 双写策略**：保留 filesystem（data/prose/）作为 primary storage + SQLite chapters 表作为 metadata index（title/created_at/updated_at）。用户内容不丢失，metadata 可查询。
2. **LLM cache 双层**：
   - L1 内存 prefix cache（atomic.Int64 计数，按 SHA256(prompt) → response 内存 map）
   - L2 SQLite llm_cache 表（persistent，重启不丢）
   - Lookup 顺序：L1 → L2 → LLM API
3. **migration 兼容**：现有 4 个表（users/sessions/audit_log/project_memberships）不动，新增 schema003/004/005 添加 P1 表。
4. **数据迁移路径**：现有内存 ProjectStore 数据导出 JSON，启动时从 JSON 导入 SQLite（一次性迁移脚本，参考 cmd/migrate/main.go 思路）

### 测试
- `internal/store/cache_test.go`：~15 tests（Insert/Get/Delete/Evict/LRU/Concurrent）
- 现有 chapters/projects 测试需更新用 SQLite（用 `:memory:` SQLite 跑测试）

### 验证
- 现有 299 tests 不破坏（增加 store 层 mock）
- CI 9/9 pass
- 数据迁移：手动导入测试数据 → 重启 → 数据可查

### 工作量：~3 周（**最大风险块**）

---

## Sprint 16：P1-Core 残余（roles + short_mode + deslop wrappers）

### 目标
- 建 `internal/roles/` 数据层包（5 个角色常量定义，编译时嵌入）
- 建 `internal/skills/short_mode.go`（短篇 8 节 pipeline helper）
- 建 `internal/skills/deslop.go`（去 AI 味 prompt helper）
- 13 个 SKILL.md 已有（无需新建 assets/ 文件）

### 文件清单（4 个）
- **新增** `internal/roles/roles.go`（5 个角色 RoleSummary + IsValid + Lookup）
- **新增** `internal/roles/roles_test.go`（5 tests）
- **新增** `internal/skills/short_mode.go`（8 节 pipeline stages 常量 + CompileShortStory helper）
- **新增** `internal/skills/deslop.go`（DeslopPrompt 模板函数 + ApplyDeslop pipeline stage）

### 关键决策
1. **roles 数据层职责**：纯常量定义（5 个角色），不依赖任何外部包
2. **short_mode 用 skills/pipeline 框架**：8 节 stages 是 []skills.Stage 常量，CompileShortStory 接受 input 调用 Pipeline.Run
3. **deslop 作为 skill**：写成 SKILL.md 配套的 Go wrapper，调用 pipeline 跑 deslop skill

### 测试
- `internal/roles/roles_test.go`：5 tests（5 角色 lookup）
- `internal/skills/short_mode_test.go`：4 tests（8 节 stage 顺序 / 变量引用）
- `internal/skills/deslop_test.go`：3 tests（prompt 模板生成）

### 验证
- 现有 299 tests 不影响

### 工作量：~1 周（低风险）

---

## Sprint 17：P1-Web 残余（admin + static + 命名整理）

### 目标
- 建 `internal/api/admin.go`（聚合 admin 路由，admin handler 单独文件）
- 建 `internal/api/static.go`（SPA mount + fallback to index.html）
- 把 users CRUD 从 auth.go 拆到 `internal/api/users.go`
- 改 `internal/api/audit_handler.go` → `audit.go`（符合 migration-mapping 命名）

### 文件清单（7 个）
- **新增** `internal/api/admin.go`（AdminHandler + 路由分发）
- **新增** `internal/api/admin_test.go`（4 tests，admin 鉴权）
- **新增** `internal/api/static.go`（StaticHandler + SPA fallback）
- **新增** `internal/api/static_test.go`（3 tests，SPA fallback + missing file）
- **新增** `internal/api/users.go`（从 auth.go 拆出 users CRUD）
- **新增** `internal/api/users_test.go`（6 tests，users CRUD）
- **重命名** `internal/api/audit_handler.go` → `internal/api/audit.go`（同步 _test.go）

### 关键决策
1. **admin 路由聚合**：从 registerOpsRoutes 抽 admin 逻辑到 AdminHandler（避免 router.go 复杂度再涨）
2. **SPA mount fallback**：所有未匹配路由返回 index.html（前端 React Router 处理）
3. **static 测试**：测试缺前端 dist 时的 fallback（404 → index.html）
4. **users 拆分**：从 auth.go 抽出，避免单文件超过 15KB

### 测试
- `internal/api/admin_test.go`：4 tests（requireAdmin + 路由分发）
- `internal/api/static_test.go`：3 tests（mount + SPA fallback + 404）
- `internal/api/users_test.go`：6 tests（CRUD + admin only）
- `internal/api/audit_test.go`：从 audit_handler_test.go rename

### 验证
- 现有 api tests（~146）不受影响
- 路由注册从 router.go 抽出到 admin.go

### 工作量：~1.5 周（中风险，需要小心不要破坏 router 注册）

---

## Sprint 18：P1-CLI 工具（5 子命令）

### 目标
- 建 `cmd/cli/` 包 + 5 子命令（skills/roles/write/cache_migrate/web）
- 子命令用 std flag（**避免引入 cobra**，减少 go.mod 依赖）

### 文件清单（6 个）
- **新增** `cmd/cli/main.go`（根命令 + subcommand dispatch）
- **新增** `cmd/cli/skills.go`（列出所有 SKILL.md）
- **新增** `cmd/cli/roles.go`（列出 5 个角色）
- **新增** `cmd/cli/write.go`（单次写作生成）
- **新增** `cmd/cli/cache_migrate.go`（cache 迁移：从内存到 SQLite）
- **新增** `cmd/cli/web.go`（启动 web，等同 `cmd/server`）

### 关键决策
1. **不用 cobra 用 std flag**：避免 go.mod toolchain 升级（Go 1.22 + toolchain 锁）
   - 主命令：`go run ./cmd/cli skills` / `roles` / `write "prompt"` / `cache-migrate` / `web`
   - 子命令 flag 用 `os.Args[1]` 判断
2. **复用现有包**：cli 包 import internal/{llm,skills,roles,store} 复用逻辑
3. **write 子命令**：直接调 llm.Router.Chat，输出到 stdout

### 测试
- `cmd/cli/main_test.go`：3 tests（子命令路由 + unknown command + help）
- 各子命令不需要 unit test（太薄，依赖 LLM；E2E 测试）

### 验证
- `go run ./cmd/cli skills` 输出 13 个 SKILL.md
- `go run ./cmd/cli roles` 输出 5 角色

### 工作量：~1 周（低风险）

---

## Sprint 19：P1-Tools（migrate + ci_status + prod-deploy-go skill）

### 目标
- 建 `cmd/migrate/main.go`（Python auth.db → Go SQLite 数据迁移）
- 建 `scripts/ci_status.go`（CI 监控，依赖 GHCR_TOKEN）
- 建 `prod-deploy-go` skill（通过 SSH 部署 + GitHub REST API）

### 文件清单（3 个 + 1 个 skill）
- **新增** `cmd/migrate/main.go`（读 Python auth.db → 写 Go SQLite）
- **新增** `scripts/ci_status.go`（调 GH Actions API）
- **新增** `~/.workbuddy/skills/prod-deploy-go/SKILL.md`（skill 入口）
- **新增** `~/.workbuddy/skills/prod-deploy-go/scripts/deploy.sh`（SSH 部署脚本）

### 关键决策
1. **migrate 工具**：用 `modernc.org/sqlite` 跨平台；Python auth.db schema 已知（users/sessions/audit_log）
2. **ci_status.go**：参考 novel2all 项目已存在的 `scripts/ci_status.py`，改成 Go 版
3. **prod-deploy-go skill**：跟 novel2all 的 prod-deploy skill 平行（一个 deploy 一次）
   - 监听 GitHub release（用 GHCR_TOKEN）
   - SSH 部署到 192.168.3.106（用户已确认端口 443 + id_ed25519）

### 测试
- `cmd/migrate/main_test.go`：3 tests（schema 兼容 + 空 db + 数据导入）
- `scripts/ci_status_test.go`：2 tests（HTTP 401 + 成功）

### 验证
- 现有 auth.db（如果有）可被导入到 Go SQLite
- ci_status.go 跑 GHCR_TOKEN 后能看到 runs

### 工作量：~1.5 周（中风险）

---

## Sprint 20：收尾（验证 P1 100%）

### 目标
- 跑最终盘点 + 验证 100%
- 更新 docs/migration-mapping.md 把所有 ⏳ 改成 ✅
- 更新 memory

### 文件清单
- **修改** `docs/migration-mapping.md`（状态表更新）
- **修改** `D:/OHMYSTORY/.workbuddy/memory/2026-09-17.md`（P1 完成记录）

### 验证
- 重跑盘点脚本
- 全部 55 个规划文件存在
- 299 + 新增 tests 全过
- CI 9/9 pass

### 工作量：~0.5 周

---

## 执行顺序与依赖图

```
Sprint 15 (DB + cache) ──┬──> Sprint 17 (Web) ──> Sprint 20 (收尾)
                         │
Sprint 16 (Core)    ─────┤
                         │
Sprint 18 (CLI)      ────┤
                         │
Sprint 19 (Tools)    ────┘
```

- Sprint 15 必须最先（其他 sprint 可能用到新 DB tables）
- Sprint 16/18/19 可并行（在 Sprint 15 完成后）
- Sprint 17 在 15 完成后（依赖 chapters SQLite）
- Sprint 20 必须最后

---

## 总体风险评估

| 风险 | 缓解 |
|---|---|
| Sprint 15 破坏 projects/chapters API 行为 | 用 `:memory:` SQLite 跑测试 + 现有 API 测试覆盖 + 数据迁移路径 |
| Sprint 17 SPA mount 没前端 dist | 测试 fallback 逻辑，部署时 web/dist 由 P2-A 提供 |
| Sprint 18 cobra 引入新依赖 | 决定用 std flag |
| Sprint 19 prod-deploy-go skill 在 wrong location | 用 ~/.workbuddy/skills/ 而非项目内 |

---

## 备注：完成度演进

| 阶段 | 完成度 |
|---|---|
| Sprint 14 完成时（之前断言 100%）| **58%**（实际）|
| Sprint 15 完成后 | ~65% |
| Sprint 16 完成后 | ~70% |
| Sprint 17 完成后 | ~76% |
| Sprint 18 完成后 | ~82% |
| Sprint 19 完成后 | ~90% |
| Sprint 20 完成后 | **100%** |

---

## 下一步

**等你确认**：
- 接受这个 6 sprint 计划？
- 或调整 sprint 顺序 / 工作量 / 跳过某些项？
- 一旦确认，从 Sprint 15（最大风险块）开始，每个 sprint 走标准流程：
  1. 写代码
  2. 跑 gofmt + build + test
  3. commit + push
  4. 监控 CI
  5. 修 CI（如有）
  6. 完成后做严格的盘点验证（不再脑记列表）

