---
name: story-setup
description: "novel2all 项目环境初始化。检查工作目录结构、初始化 _tracking-state.json、创建创作设定模板。"
status: full
since: v0.30.0
---

> Go 端调整 (Sprint 33): 删除 Python V1 的 Agent IDE 集成段（`.claude/.codex/.zcode/.agents` 等），
> Go 端不部署这些目录，原文不再适用。

# story-setup：novel2all 项目初始化

只初始化或校验 novel2all workspace，不部署任何 Agent 平台文件。

## 流程

1. **检查工作目录**
   - 如果 `{project_root}/_tracking-state.json` 存在 → 已初始化，问用户要"重新初始化"还是"补全缺失文件"
   - 不存在 → 进入初始化流程

2. **收集项目信息**
   - 书名
   - 题材（玄幻 / 都市 / 言情 / 短篇 / ...）
   - 文风锚点（古风古韵 / 现代口语 / 冷硬 / ...）
   - 目标字数（如 100 万字）
   - 目标章节数（如 400 章）

3. **初始化项目目录结构**
   - `设定/文风.md`
   - `设定/世界观/`（可后续补充）
   - `设定/角色/`（可后续补充）
   - `大纲/`
   - `正文/`
   - `拆文库/`
   - `.novel2all/`

4. **初始化 _tracking-state.json**
   - 调 `Tracker.init()`
   - 写入 TrackingState schema

5. **创建 创作设定.md 模板**
   - 用户初始设定

## 关键原则（Go 端）

- **不要覆盖已有文件**——已有正文、设定、大纲必须保留
- **Go 端不涉及 IDE 集成目录**（`.claude/.codex/.zcode/.agents` 属 Python V1 规范，Go 端跳过）
- **不要生成 platform agent**——所有 role 通过 Go 端内置 `internal/roles/roles.go` 注册
- **不要配置外部模型 / 权限 / Session**——这些由 `internal/config/` 配置层管

## 长记忆系统提示

novel2all 的 tracking 系统是**自动维护**的：
- 写完一章后自动用 Extractor 提取关键信息
- 自动 merge 到 _tracking-state.json
- 不要让用户手工更新 tracking 文件

## 调用示例

```
用户：/story-setup
novel2all：
  1. 检查 ./_tracking-state.json
  2. 不存在 → 问"书名 / 题材 / 文风 / 目标字数"
  3. 创建项目结构 + 初始化 tracking
  4. 输出："项目已初始化。运行 /story-long-write 开始创作。"
```

```
用户：/story-setup
novel2all：
  1. 检查 ./_tracking-state.json
  2. 已存在 → "项目已初始化。运行 /story-long-write 或 /story-import 继续。"
```
