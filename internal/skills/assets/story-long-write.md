---
name: story-long-write
description: "novel2all 长篇写作：题材定位、大纲搭建、人物设定、正文输出。使用 5 层 memory 系统保持长篇一致性。"
---

# story-long-write：长篇写作

novel2all 的长篇写作能力。所有长篇相关任务（写大纲 / 写角色 / 写细纲 / 写正文）都走这个 skill。

## 长记忆系统（核心）

novel2all 通过 5 层 memory 系统解决长篇"LLM 跑偏"问题：

### L1: 核心设定（永远加载）
- `设定/文风.md`
- `设定/世界观/`（力量体系 / 地理 / 背景）

### L2: 角色状态（按需加载）
- `_tracking-state.json#characters` 的当前快照
- 每个角色：位置 / 情绪 / 动机 / 已知

### L3: 最近章节（滑动窗口）
- 最近 5 章的摘要（不是全文）
- 自动从 _tracking-state.json#recent_chapter_summaries 提取

### L4: 事件检索（向量检索，v0.21+）
- 本章主题相关的历史事件
- top-K 召回

### L5: 知识图谱（tool call）
- 角色关系 / 伏笔链 / 时间线
- LLM 通过 novel2all_internal_graph_query tool 主动查询

## 工作流

### 写细纲前

1. **加载 MemoryContext**：`MemoryManager.load_for_writing(chapter, outline, characters)`
2. **pre-write 一致性检查**：`Verifier.pre_write_check(state, outline)`
3. 如果 critical 问题 → 阻断，让用户先解决
4. 加载细纲相关 references（按需）

### 写细纲

1. 调 LLM 生成细纲（含情节节点 / 人物出场 / 钩子）
2. 保存到 `大纲/细纲_第NNN章.md`
3. 触发 Extractor 提前提取本章涉及的"角色状态预期变化"

### 写正文前

1. 再次加载 MemoryContext（细纲已写完）
2. 强制要求细纲存在（pre-write gate）

### 写正文

1. 加载核心 + 角色 + 最近 + 相关事件
2. 拼装 prompt：
   - System: 5 层 memory 内容
   - User: 本章细纲 + 写作要求
3. 调 LLM（流式输出）
4. 每 500 字调一次轻量校验

### 写完后

1. **自动提取**：调 Extractor 提取本章关键信息
2. **merge 到 tracking**：自动更新 _tracking-state.json
3. **post-write 检查**：调 Verifier 检查质量
4. **保存正文**：写入 `正文/第NNN章.md`
5. **生成摘要**：加入 `recent_chapter_summaries`

## 关键约束

- **必须先有细纲**——pre-write gate 阻断（blocking）
- **不要乱写超 100 字"自由发挥"**——所有内容必须基于细纲
- **不要忽略 memory context**——尤其 L2 角色状态
- **不要写伏笔不记录**——埋的伏笔必须由 Extractor 提取

## 文风锚点

- 调 `设定/文风.md` 作为 baseline
- 偏离 baseline 时 post-write 会 warning
- AI 味（套话 / 总结性语句）会被 story-deslop 拦截

## References 加载

按需加载（不在 prompt 里塞所有）：

```
references/大纲排布.md        # 必读
references/角色设计.md        # 写新角色时
references/钩子技法.md        # 写章尾时
references/对话技法.md        # 对白为主时
references/对标/{书名}/      # 有对标参考书时
```

## 输出

每章完成产出：
- `正文/第NNN章.md`（正文）
- `_tracking-state.json`（更新）
- `大纲/细纲_第NNN章.md`（已有）
- 后处理 hooks 输出（去 AI 味结果 / 一致性报告）

## 调用示例

```
用户：/story-long-write 写第5章
novel2all：
  1. 加载 memory context（5 层）
  2. pre-write check（检查细纲存在 + 一致性）
  3. 细纲缺失 → 提示先写细纲
  4. 加载细纲 + 文风 + 角色状态 + 最近章节
  5. 调 LLM 生成第 5 章正文（流式）
  6. post-write check
  7. 自动 Extractor 提取 + 更新 tracking
  8. 保存到 正文/第005章.md
```
