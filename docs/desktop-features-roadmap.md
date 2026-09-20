# Novel2ALL Desktop - 后端能力 Roadmap

**目标**: 把 novel2all-go 后端能力逐步搬到 Novel2ALL 桌面 app
**原则**: 分模块, 每个模块独立 PR + commit + 文档同步 + CI 验证
**当前状态**: Phase 1-4 完成 + Module F (项目 CRUD) + Module A (章节 CRUD) + Module B (LLM API key 配置) + Module C (章节 action LLM 调用) ✅

---

## 后端能力清单 (按优先级)

### Module A: 章节 CRUD (基础编辑器) ← **从这开始**
**目标**: 用户能创建/编辑/删除章节, 桌面 app 真正能用
**后端端点**:
- `POST /api/projects/{id}/chapters/save` (已有 save 端点, 验证可写)
- `POST /api/projects/{id}/chapters/{n}/delete` (verify)
- `GET  /api/projects/{id}/chapters/{n}` (verify)

**桌面 app 加**:
- EditorPage: Monaco editor 或简单 textarea
- Save / Delete 按钮
- 自动保存 (Phase B 后期)

**工作量**: 1.5 天
**风险**: 中 — 编辑器要选型 (Monaco 体积大, plain textarea 简单)
**验证**: 后端→前端往返保存→读取一致

---

### Module B: LLM API key 配置 ✅ (commit bb14af4, 2026-09-20)
**目标**: 用户能在桌面 app 设置 LLM key (dashscope/deepseek/minimax), key加密存本地
**实现**:
- 后端: 无改动 (admin key 仍作 fallback, Module B.2 才消费 user keys)
- 桌面 app `desktop/internal/secrets/`: AES-256-GCM + PBKDF2-SHA256 (100k iter), master key 从 (hostname + username) 派生 (跨机器/跨用户不可解), JSON 文件 + AAD 防改名
- SettingsPage "LLM API Keys" section: 3 个 provider 行 (dashscope/deepseek/minimax), 各有 password input + 保存/删除按钮 + "已配置 ✓" 状态 badge + "清除全部" 按钮
- 5 个 wails-bound 方法: SupportedProviders / GetLLMKeys / SetLLMKey / ClearLLMKeys / HasLLMKey
- 14 个单元测试全过 (含 race detector + AES-GCM 篡改检测 + AAD 重命名检测 + 跨机器密钥不同)

**安全限制 (MVP)**:
- 防 casual 访问: 同机同用户可解, 其他用户/机器不可解
- **不**防恶意 root / 同用户进程 / 物理访问攻击者
- Module B.2 计划升级到 OS keystore (DPAPI / Keychain / libsecret)

**下一步 Module B.2** (1d):
- 桌面请求时加 X-LLM-Key-{provider} header
- 后端接受 user-provided key 覆盖环境变量 admin key (per-request)
- 用户 key 优先级 > admin key

---

### Module C: 章节 action (LLM 调用) ✅ (commit 41b76bb, 2026-09-20)
**目标**: 用户能从桌面 app 调 LLM 改章节
**实现**:
- 后端 5 端点 (已有): `/api/chapter/{N}/{action}/` expand/rewrite/review/insert/rollback
- 桌面 Go: 5 个 wails 方法 + 通用 callChapterAction() helper
- 桌面 React: AI 辅助 section (instruction + position 输入 + 5 按钮) + Review modal (大分数 + verdict + issues 3 段)
- http timeout 30s → 120s (LLM 调用慢)
- 后端返 ActionResponse (4 改文件 action) / ReviewResult (review 不改文件)
- 端到端验证: POST /api/chapter/1/review → 200 + 完整 JSON shape

**下一步 Module B.2** (0.5-1d): 用户配置的 key (Module B) 用于 LLM 调用, 桌面传 X-LLM-Key-{provider} header, 后端覆盖 admin key

---

### Module D: 人物/关系/伏笔 CRUD ✅ (2026-09-20)
**目标**: 用户能管理 characters / relationships / foreshadows
**实现**:
- 后端补 PUT/PATCH/DELETE (原只有 GET/POST): ServeHTTP 支持 path 长度==2, 加 handleGet/Update/Delete
- 桌面 Go: 3 struct + 15 wails 方法 (List/Get/Create/Update/Delete × 3 类别)
- 通用 callKnowledgeCRUD helper 简化 15 个方法
- React UI: KnowledgePanel (3 tab: 人物/关系/伏笔), 列表 + 创建/编辑表单 + 删除
- 触发: user-bar "📚 知识管理" 按钮 (仅选中项目后显示)

**已知限制**:
- 数据存后端 state/<category>.json (Phase 1 mock, 后续 project_id 绑定文件系统路径时升级)
- Relationship 用人物名字引用 (不是 ID), 简单但脆弱 (重命名后失效)

**工作量**: 2 天
**风险**: 低 — 简单 CRUD
**验证**: 增删改查 5 个 entity 都通

---

### Module E: 章节大纲 (outline)
**目标**: 用户能看/编辑章节大纲 (与 chapter 关联)
**后端**: `/api/projects/{id}/outline` (需查)
**桌面 app 加**:
- OutlinePage: 大纲列表 + 编辑
- 章节选择器关联大纲

**工作量**: 1.5 天
**风险**: 中
**验证**: 编辑大纲 → 章节页面能读到

---

### Module F: 项目 CRUD (含 owner check)
**目标**: 用户能创建/编辑/删除 project
**后端**: `/api/projects` POST/PUT/DELETE (已有)
**桌面 app 加**:
- NewProjectModal / EditProjectModal
- 项目列表加 "+" 按钮

**工作量**: 1 天
**风险**: 低
**验证**: 创建→列表出现→编辑→删除→消失

---

### Module G: Memory 流式 (上传/下载快照)
**目标**: 用户能上传/下载项目状态快照到后端同步
**后端**: `/api/projects/{id}/snapshot` (已有基础)
**桌面 app 加**:
- "同步到云端" 按钮
- 进度条 + 冲突解决 UI

**工作量**: 3 天
**风险**: 高 — 冲突处理 + 版本号
**验证**: 上传→换设备下载→数据一致

---

### Module H: Skills 调用
**目标**: 用户能从桌面 app 调 13 个 skills (expand/review 等可走 skills 路由)
**后端**: 已有 skills API
**桌面 app 加**:
- SkillsPicker 组件
- Skills 列表 + 描述 + 触发

**工作量**: 1.5 天
**风险**: 低
**验证**: 调 skill → 看后端日志

---

### Module I: 设置页扩展 (主题/语言/快捷键)
**目标**: SettingsPage 加 UI 偏好
**后端**: 无 (本地存 %APPDATA% JSON)
**桌面 app 加**:
- ThemePicker (light/dark/auto)
- LanguagePicker (zh/en)
- KeyBindings 表格

**工作量**: 2 天
**风险**: 低 — 纯前端
**验证**: 改主题立即生效, 重启保留

---

### Module J: 自动启动 + 最小化到托盘
**目标**: 桌面 app 开机自启 + 默认最小化
**后端**: 无
**桌面 app 加**:
- Windows Registry 自启 (HKCU\...\Run)
- 设置项 "开机自启" toggle

**工作量**: 0.5 天
**风险**: 低 — 注册表操作可逆
**验证**: 勾选→重启 Windows→app 自动跑

---

## 优先级排序

### 第一批 (核心写作能力, 1 周)
1. **Module A** 章节 CRUD (1.5d) ← 最优先, 用户能写作
2. **Module F** 项目 CRUD (1d) ← 先有项目才能有章节
3. **Module B** LLM key 配置 (1d) ← 后续 action 需要

### 第二批 (LLM 集成, 3 天)
4. **Module C** 章节 action (2d)
5. **Module H** Skills 调用 (1.5d)

### 第三批 (知识管理, 3.5 天)
6. **Module D** 人物/关系/伏笔 (2d)
7. **Module E** 章节大纲 (1.5d)

### 第四批 (体验优化, 2.5 天)
8. **Module I** 设置页扩展 (2d)
9. **Module J** 自动启动 (0.5d)

### 第五批 (云同步, 3 天)
10. **Module G** Memory 流式 (3d)

---

## 每个模块的开发流程

### 标准 commit sequence

```
1. 调研后端现有端点 + 写 desktop Go method + 前端 UI
2. go vet + gofmt + go build (本地验证)
3. 写/补单元测试 (internal/xxx/xxx_test.go)
4. git add + git commit (atomic, 不混其他改动)
5. git push origin main
6. 查 GitHub CI (curl /actions/runs?per_page=1)
7. 改 desktop-development.md changelog
8. git add docs/ + git commit + git push
```

### 强制规则

- ❌ 不混多个 module 在一个 commit
- ❌ 不写多于 1 个新 module 文件的 commit (除 module A 完整闭环)
- ✅ 每个 module 有 commit hash + CI # + 文档登记
- ✅ build + vet + gofmt + 测试 全绿才 push
- ✅ push 后立即 curl /actions/runs 查 CI

### 何时停 + review

- 一个 module 完成后, 跟用户确认"这个 module OK 吗? 要继续下一个吗?"
- 用户可以临时改方向
- 用户可以跳到任意 module

---

## 当前准备状态

| Module | 状态 |
|--------|------|
| A 章节 CRUD | ✅ 已完成 (2026-09-19, commits 3842005/b95b6ec/5d4f3d3) |
| B LLM key | ✅ 已完成 (2026-09-20, commit bb14af4) |
| C 章节 action | ✅ 已完成 (2026-09-20, commit 41b76bb) |
| D 人物/关系/伏笔 | ✅ 已完成 (2026-09-20, 19f3a2f) |
| E 章节大纲 | ⏸️ 排队 |

**当前 main 分支**: 19f3a2f (Module D 完成)
**GitHub v0.1.0 release**: 已发布, Novel2ALL.exe 11.7 MB

---

## 开发依赖关系

### 代码依赖 (API 调用)

```
A 章节 CRUD — 无依赖
F 项目 CRUD — 无依赖
B LLM key — 无依赖
C 章节 action — 依赖 B (需 LLM key 才能调 LLM API)
H Skills 调用 — 依赖 B
D 人物/关系/伏笔 — 无依赖 (与 A/F/B 同级别, CRUD 同模式)
E 章节大纲 — 无依赖 (独立 collection)
I 设置页扩展 — 无依赖
J 自动启动 — 无依赖
G Memory 流式 — 依赖 A + F (snapshot 包含章节 + 项目数据)
```

### 业务依赖 (用户操作)

```
用户操作流程:
  登录 → 项目列表 (需 Module F) → 章节列表 (需 Module A) → 编辑 (Module A)
```

**Module A 业务上需要 Module F** (用户先创建项目, 后创建章节).
**Module F 业务上不需要 Module A** (创建项目不依赖章节).

### 实现顺序

**第一批** (1 周, 全部无代码依赖):
- Module A 章节 CRUD (1.5d) — 业务上需 F 先完成
- Module F 项目 CRUD (1d) — **可与 A 并行, 但建议先 F** (UI 流程先有项目列表)
- Module B LLM key (1d)

**推荐顺序**: F → A → B (F 先, A 复用 F 的 UI 框架, B 独立)

**第二批** (3 天, 依赖 B):
- Module C 章节 action (2d)
- Module H Skills 调用 (1.5d)

**第三批** (3.5 天, 无依赖):
- Module D 人物/关系/伏笔 (2d)
- Module E 章节大纲 (1.5d)

**第四批** (2.5 天, 无依赖):
- Module I 设置页扩展 (2d)
- Module J 自动启动 (0.5d)

**第五批** (3 天, 依赖 A + F):
- Module G Memory 流式 (3d)

---

## 我立即做的事

按你要求"开发前先输出开发计划文档":

✅ 已写本文档
⏸️ 等你确认 Module A 开工 (或调整顺序)

---

**维护**: novel2all-bot
**最后更新**: 2026-09-19 (按 desktop-development.md 同一规则)