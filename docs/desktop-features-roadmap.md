# Novel2ALL Desktop - 后端能力 Roadmap

**目标**: 把 novel2all-go 后端能力逐步搬到 Novel2ALL 桌面 app
**原则**: 分模块, 每个模块独立 PR + commit + 文档同步 + CI 验证
**当前状态**: Phase 1-4 完成 (login + project/chapter 列表 + 自动更新 + NSIS 打包)

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

### Module B: LLM API key 配置
**目标**: 用户能在桌面 app 设置 LLM key (OpenAI/Anthropic/DashScope/DeepSeek), key加密存本地
**后端**: 新建 `POST /api/auth/llm-keys`, 存 encrypted blob (AES-GCM, key 派生用户密码)
**桌面 app 加**:
- SettingsPage 加 LLM Keys 区块
- 各 provider 输入框 + 保存按钮

**工作量**: 1 天
**风险**: 中 — 加密方案 + 跨设备同步策略
**验证**: 设置 → 重启 → key 还在 → 调 LLM 调用成功

---

### Module C: 章节 action (LLM 调用)
**目标**: 用户能调 LLM 改章节 (expand/rewrite/review/insert/rollback)
**后端**: `/api/projects/{id}/chapters/{n}/{action}` 5 端点 (已有 mock)
**桌面 app 加**:
- ChapterAction 按钮组 (5 个 action)
- LLM 调用进度显示 (SSE or polling)

**工作量**: 2 天 (Mock LLM + UI 流程)
**风险**: 高 — SSE 流式 + 错误处理
**验证**: 点 expand → 看到生成内容 → 保存到磁盘

---

### Module D: 人物/关系/伏笔 CRUD
**目标**: 用户能管理 characters / relationships / foreshadows
**后端**: `/api/projects/{id}/characters`, relationships, foreshadows (已有)
**桌面 app 加**:
- CharactersPage + RelationshipsPage + ForeshadowsPage
- 表单 (add/edit/delete)

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
| A 章节 CRUD | 🟡 待启动 (用户刚批准) |
| B LLM key | ⏸️ 排队 |
| ... | ⏸️ 排队 |

**当前 main 分支**: 488f5f9 (Phase 4 完成)
**GitHub v0.1.0 release**: 已发布, Novel2ALL.exe 11.7 MB

---

## 开发依赖关系

```
A 章节 CRUD (无依赖)
  ↓
F 项目 CRUD (无依赖, 可与 A 并行)
  ↓
B LLM key (无依赖, 单独)
  ↓
C 章节 action (依赖 B LLM key)
  ↓
H Skills 调用 (依赖 B, 可与 C 并行)
  ↓
D 人物/关系/伏笔 (依赖 F 项目 CRUD)
  ↓
E 章节大纲 (依赖 A)
  ↓
I 设置页扩展 (无依赖, 单独)
  ↓
J 自动启动 (无依赖, 单独)
  ↓
G Memory 流式 (依赖 A + F)
```

**第一批** (A + F + B) 无依赖, 可并行
**第二批** (C + H) 依赖 B
**第三批** (D + E) 依赖 A/F
**第四批** (I + J) 无依赖
**第五批** (G) 依赖多

---

## 我立即做的事

按你要求"开发前先输出开发计划文档":

✅ 已写本文档
⏸️ 等你确认 Module A 开工 (或调整顺序)

---

**维护**: novel2all-bot
**最后更新**: 2026-09-19 (按 desktop-development.md 同一规则)