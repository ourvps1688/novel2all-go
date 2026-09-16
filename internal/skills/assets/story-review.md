---
name: story-review
description: "novel2all 多视角审查：4 个 Role 多视角审稿（一致性 / 伏笔追踪 / 设定冲突 / 文风 / AI 味）。"
---

# story-review：多视角审查

调 4 个 Role 并行审稿，每个 Role 从不同视角检查章节质量。

## 4 个审稿 Role

| Role | 视角 | 关注点 |
|---|---|---|
| `consistency-checker` | 内部一致性 | 角色性格漂移 / 设定冲突 / 时间线硬伤 |
| `narrative-writer` | 文风与节奏 | 偏离文风 / AI 味 / 钩子弱 / 节奏拖沓 |
| `character-designer` | 人物塑造 | 角色行为合理 / 对话贴脸 / 动机连贯 |
| `story-architect` | 结构与伏笔 | 伏笔是否被收回 / 新伏笔是否埋好 / 与全局大纲对得上 |

## 工作流

1. **输入**：作者指定章节（或最近 5 章）
2. **加载 MemoryContext**：5 层 memory 全加载（包括 tracking state）
3. **并行调 4 Role**：
   - 每个 Role 独立 prompt，独立产出 review 报告
4. **聚合输出**：
   - 按 severity 排序的所有问题
   - critical 问题必须先解决
   - warning 问题给建议

## 输出格式

```
审查报告_第NNN章.md
├── 1. 综合评分（0-100）
│   ├── 一致性
│   ├── 文风
│   ├── 角色
│   └── 结构
├── 2. critical 问题（必须解决）
├── 3. warning 问题（建议修复）
├── 4. 角色逐个审查
│   ├── 林雷: 9/10 — 行为合理
│   └── 霍格: 6/10 — 第3段情绪转换过快
└── 5. 修改建议（按章节顺序）
```

## 关键原则

- **不替作者改稿**——只指出问题和建议
- **每条问题都给原文引用**——不脱离上下文的"感觉不对"
- **critical 不空喊**——必须给具体修复方法
- **不同 Role 可以意见冲突**——保留冲突让作者判断

## 调用示例

```
用户：/story-review 第5章
novel2all：
  1. 加载 memory context（包括 _tracking-state.json）
  2. 并行调 4 Role
  3. 聚合 → 输出审查报告
```
