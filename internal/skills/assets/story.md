---
name: story
description: "novel2all 网文工具箱主入口。根据用户需求自动路由到对应 skill，管理作者习惯。触发方式：/story、/网文、「我想写小说」「记住我的写作习惯」。"
status: full
since: v0.30.0
---

> Go 端调整 (Sprint 33): 删 Python IDE spawn 段（Go 端无 IDE 集成 runtime），
> 统一方法名为 CamelCase（LoadForWriting 而非 load_for_writing）。
> Go 端 RoleRegistry 通过 `roles.StageToRole()` 映射，无需 spawn 独立 agent。

# story：novel2all 工具箱路由

你是 novel2all 工具箱的路由入口。用户的请求模糊时由你分发到具体 skill。

## 路由表

| 用户意图 | 关键词示例 | 路由到 |
|---|---|---|
| 写长篇 | 开书、写大纲、长篇、连载 | `/story-long-write` |
| 写短篇 | 短篇、盐言、一万字 | `/story-short-write` |
| 长篇拆文 | 拆文、分析这本书、黄金三章 | `/story-long-analyze` |
| 短篇拆文 | 拆短篇、分析这个故事 | `/story-short-analyze` |
| 长篇扫榜 | 长篇排行、什么火、起点/番茄/晋江 | `/story-long-scan` |
| 选题决策 | 写什么能爆、帮我选题 | `/story-long-scan` |
| 短篇扫榜 | 短篇排行、知乎盐言排行 | `/story-short-scan` |
| 去 AI 味 | 去 AI 味、太 AI、去味 | `/story-deslop` |
| 审查稿件 | 审查、审稿、帮我审一下 | `/story-review` |
| 封面 | 封面、封面图 | `/story-cover` |
| 环境部署 | 准备写书、搭环境、初始化 | `/story-setup` |
| 浏览器操控 | 浏览器、抓取 | `/browser-cdp` |
| 导入小说 | 导入、反向解析 | `/story-import` |
| 查角色/伏笔/进度 | 沈栀现在什么状态、写到哪了 | spawn `story-explorer` role |
| 查资料 | 帮我查资料、调研、搜索 | spawn `story-researcher` role |

## 长记忆系统

novel2all 使用 5 层 memory 系统（详见 `internal/memory/`）：

- L1: 核心设定（永远加载）→ `ProjectStructure.SetupMD() + StyleMD() + WorldviewDir()`
- L2: 角色状态（按需加载）→ `MemoryContext.L2 (Tracker.GetCharacter per char)`
- L3: 最近章节摘要（滑动窗口）→ `MemoryContext.L3 (Tracker.GetRecentSummaries)`
- L4: 事件检索（向量检索）→ `MemoryRetriever.Query(text, topK)`
- L5: 知识图谱（tool call 查询）→ `MemoryGraph` methods（Sprint 34 暴露为 tool）

每次写正文前，调用 `MemoryManager.LoadForWriting(ctx, chapter, outline, characters)` 加载相关 memory。
写完后调用 `MemoryManager.UpdateAfterWriting(ctx, chapter, content)` 自动更新 `_tracking-state.json`。

## 工作流

```
用户输入 → 路由 → 调用对应 skill
  ├── 写正文类（story-long-write / story-short-write）
  │     ├── pre-write check（一致性）
  │     ├── 加载 memory context
  │     ├── 调 LLM 写作
  │     ├── post-write check（质量）
  │     ├── 自动提取 + 更新 tracking
  │     └── 写正文文件
  ├── 拆文类（-analyze）→ 拆文报告 + 角色卡 + 设定卡
  ├── 扫榜类（-scan）→ 趋势报告
  ├── 审查类（story-review）→ 4 角色多视角审稿
  └── 元数据类（story-setup / story-import）→ 项目结构初始化
```

## 项目结构

```
{project_root}/
├── _tracking-state.json
├── 创作设定.md
├── 设定/
│   ├── 文风.md
│   ├── 世界观/
│   └── 角色/
├── 大纲/
├── 正文/
└── 拆文库/
```

## 子 skill 调用约定（Go 端）

调用其他 skill 时：
- 加载目标 skill 的 SKILL.md 正文（embed.FS 13 个 .md 已在 `internal/skills/assets/`）
- 拼接当前 memory context（如果涉及写作）：
  - `MemoryManager.LoadForWriting(ctx, chapter, outline, characters)`
  - 返回 `MemoryContext`，调 `ToSystemSections()` 拼到 system prompt
- 调 `skills.Executor.Execute(ctx, ExecuteInput{SkillName, UserInput, Vars}, ch)` 流式执行
- **不启动独立运行时**——所有 skill 都在同一个 Go process

## 角色映射（Go 端）

Go 端 `internal/roles/roles.go` 提供 5 个 Role：
- `story_outliner`：大纲 / 结构 / 伏笔
- `chapter_writer`：章节写作 / 文风 / 节奏
- `consistency_checker`：一致性 / 设定冲突
- `story_reviewer`：多视角审稿
- `character_extractor`：人物提取

调用：`roles.StageToRole(stageName)` 返回对应 Role。

> Go 端无 Python V1 的 `story-explorer` / `story-researcher` role。
> 类似功能由 `MemoryManager.LoadForWriting`（探索）+ `MemoryRetriever.Query`（检索）替代。
