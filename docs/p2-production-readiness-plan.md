# P2 阶段开发计划：让 Go 具备真正的 AI 生产能力（2026-09-17）

> **起点**：V0.29.4（Path C 完成 + CI #98 绿 + 架构 100% 对等 Python V0.23）
> **目标**：补齐 5 个核心缺口 + 调整 13 个 SKILL.md = Go 真能跑 AI 网文/短篇生产
> **产出**：V1.0.0 production-ready

## 现状评估（基于 Sprint 22-31 严格盘点）

### ✅ 已完整（架构层）
| 子系统 | 状态 |
|--------|------|
| 4 LLM providers（OpenAI/Anthropic/DashScope/DeepSeek）+ Router | ✅ 流式 + 4 provider |
| 13 SKILL.md（embed.FS） | ✅ 完整 |
| 5 Role（story_outliner/chapter_writer/consistency_checker/story_reviewer/character_extractor） | ✅ |
| Pipeline（Stage DAG + Vars + 拓扑排序） | ✅ |
| 8 节 ShortModeStages | ✅ |
| 5 层 Memory 子系统（9 文件 + ~30 helper methods） | ✅ |
| MultiAgentReviewer（4 role 并行 + goroutine） | ✅ |
| RollbackManager（Snapshot + Rollback + ListSnapshots） | ✅ |
| ProjectStructure（15 methods） | ✅ |
| Exporter（md/txt/epub/pdf + Chapter + BookMetadata + ExportBookEPUB） | ✅ |
| SSE handler（race 已修） | ✅ |
| CLI（8 子命令）+ cmd/migrate + scripts/ci_status | ✅ |
| Tests：632 PASS / 0 FAIL | ✅ |

### ⚠️ 5 个核心缺口（影响"真生产"）
| # | 缺口 | 影响 |
|---|------|------|
| 1 | `handleStream` 是 mock（write.go 推 fake chunk） | 长篇章节写不出真内容 |
| 2 | Memory 5 层未自动注入 prompt（LoadForWriting 在但 handleStream 没调） | LLM 跑偏（失去长篇一致性） |
| 3 | LLM tool call 未实现（`novel2all_internal_graph_query` 缺失） | LLM 不能主动查 graph |
| 4 | 文风/世界观/references 自动加载未实现 | 失去作者风格延续 |
| 5 | chapter_actions（expand/insert/rewrite/review）是 mock | 5 个 action 不能真改稿 |

### ⚠️ 13 SKILL.md 调整
| 文件 | 问题 |
|------|------|
| `story-review.md` | Python 4 role 名（narrative-writer/character-designer/story-architect）不匹配 Go 5 role |
| `story.md` | `spawn story-explorer / story-researcher role` Go 端无；方法名 snake_case 应改 CamelCase |
| `story-setup.md` | Agent IDE 集成段（`.claude/.codex/.zcode`）Go 端无意义 |
| `browser-cdp.md` | Python 代码示例（async_playwright）Go 跑不了 |
| `story-long-scan.md` / `story-short-scan.md` | `references/genre-trends.md` 缺失 |
| `story-long-write.md` | L5 tool call 未实现标注 |

## 总览

| Sprint | 主题 | 文件数 | 工作量 | 风险 | 依赖 |
|--------|------|--------|--------|------|------|
| 32 | handleStream 真化（核心） | 4 新 + 5 改 | ~1.5 周 | **中**（memory token 截断） | 无 |
| 33 | SKILL.md 调整 | 13 改 | ~0.5 周 | 低 | 无（可与 32 并行） |
| 34 | Tool call + chapter actions 真化 | 6 新 + 4 改 | ~1 周 | 中（4 provider 兼容） | 32 |
| 35 | references + 文风/世界观自动加载 | 12 新 + 3 改 | ~1 周 | 低 | 32 |
| 36 | E2E 集成测试 + 真实项目验证 | 5 新 + 4 改 | ~0.5 周 | 低 | 32-35 |
| **合计** | | **40+ 文件** | **~4.5 周** | | |

> Sprint 依赖图：
> ```
> Sprint 32 (handleStream) ─┬─→ Sprint 34 (tool call + actions) ─┐
>                          ├─→ Sprint 35 (references + settings)  ├─→ Sprint 36 (E2E)
> Sprint 33 (SKILL.md) ─────┘                                   │
>                                  (并行不依赖 32)              ─┘
> ```

---

## Sprint 32：handleStream 真化（核心，1.5 周）

### 目标
`/api/write/stream` 真正按 SKILL.md 工作流跑：
1. pre-write check（一致性 + 细纲存在）
2. 加载 5 层 memory context
3. 加载文风/世界观/相关 references
4. 调 LLM 流式生成（真 prompt 而非 mock）
5. post-write check
6. 自动 Extractor 提取 → 更新 _tracking-state.json
7. SSE event 序列与 SKILL.md 完全对齐

### 文件清单（9 个）

**新增**
- `internal/api/write_e2e_test.go`（E2E 测试：mock LLM + mock 项目目录）

**重写（核心）**
- `internal/api/write.go`：`handleStream` 全重写
  - L120-253 全部替换为：pre-write → memory → load refs → LLM stream → post-write → extract → save
  - 加 `WriteHandler` 字段：`memMgr *memory.MemoryManager`、`refLoader *references.Loader`（**指针 nil 时降级**）
  - 加 `executeWithMemory(ctx, task, chapter, outline, characters)` wrapper
- `internal/memory/types.go`：加 `MemoryContext.ToSystemSections()` 拼接实现（L1-L5 编号去掉，纯 sections 顺序）

**修改**
- `internal/api/sse.go`：
  - `handleWriteStream` 内部 mock 删除，改调 `WriteHandler.handleStream`
  - `handleWriteStreamModel` 同上
  - `mockSSEWrite` 保留（CLI smoke 用），但实际 handler 不再用
- `internal/server/server.go`：`WriteHandler` 注入 `memMgr` + `refLoader`
- `internal/api/chapter_actions.go`：暂时不动（Sprint 34 处理）

### 关键决策

1. **WriteHandler 依赖注入**：
   ```go
   type WriteHandler struct {
       tasks     *TaskManager
       executor  *skills.Executor
       loader    *skills.Loader
       taskMgr   *PipelineTaskManager
       memMgr    *memory.MemoryManager  // 新增（nil = 跳过 memory）
       refLoader *references.Loader     // 新增（Sprint 35 才有）
   }
   ```
   不破坏现有 `NewWriteHandler(executor, loader)` 签名 → 加 `NewWriteHandlerWithMemory(...)`。

2. **System prompt 拼接顺序**（去掉 L1-L5 编号）：
   ```
   [设定/文风.md]            ← ProjectStructure.StyleMD()
   [设定/世界观/*]           ← ProjectStructure.WorldviewDir() 遍历
   [角色状态]                ← MemoryContext.L2 (GetCharacter per char)
   [最近 5 章摘要]            ← MemoryContext.L3
   [相关历史事件 top-K]        ← MemoryContext.L4 (MemoryRetriever.Query)
   [剧情结构]                ← references/大纲排布.md
   ```
   每个 section 之间 `---` 分隔。token 超 8K 时按重要性截断（文风 + 最近章节 必保留）。

3. **Pre-write gate（blocking）**：
   ```go
   if !skipPreWrite {
       outline, err := loadOutline(projectRoot, chapter)
       if os.IsNotExist(err) {
           // SSE event: pre_write_check failed
           sseError(w, flusher, "outline_missing", "请先写细纲 (大纲/细纲_第NNN章.md)")
           return  // 不调 LLM
       }
       if mm != nil {
           result, _ := mm.PreWriteCheck(state, outline)
           if result.HasCriticalIssues() {
               sseError(w, flusher, "pre_check_failed", result.Summary())
               return
           }
       }
   }
   ```

4. **"每 500 字轻量校验"**改为 `ticker(1秒) progress event`：
   - SKILL.md 原文说"每 500 字调一次轻量校验"——但 500 字后停下来校验会打断流式
   - Go 端改用 1 秒 ticker 推送 `progress` event（含累计字数），校验放 post-write 一次跑

5. **Extractor + Tracker 自动更新**：
   ```go
   if mm != nil && len(accumulated) > 0 {
       extracted, _ := mm.Extractor().Extract(state, chapter, accumulated)
       mm.Tracker().Merge(extracted)
       _ = mm.Tracker().Write()  // atomic JSON write
   }
   ```

### SSE event 序列（与 SKILL.md 对齐）
```
started        → {task_id, chapter, skill, min_chars, skip_pre_write, project_root}
pre_write_check → {status: "ok" | "outline_missing" | "critical_issues", notes}
chunk          → {text: "..."}  （流式 LLM chunk）
progress       → {chars_so_far, target_chars, phase: "writing"|"extracting"|"verifying"}
post_write_check → {status: "ok"|"issues", notes, issues: [...]}
done           → {task_id, chapter, content_chars, output_path, content}
cancelled      → 客户端断开时
error          → {message}
```

### 测试（5 个）

- `internal/api/write_test.go`：
  - `TestHandleStream_OutlineMissing`（细纲缺失返回 SSE error 不调 LLM）
  - `TestHandleStream_PreCheckCritical`（critical issues 阻断）
  - `TestHandleStream_LLMStreamChunks`（mock LLM 推 3 chunk → 验证 SSE 序列正确）
  - `TestHandleStream_PostCheckRuns`（完成后调 verifier）
  - `TestHandleStream_ExtractorUpdatesTracker`（mock 项目 + 验证 _tracking-state.json 更新）
- `internal/api/write_e2e_test.go`：
  - `TestWriteE2E_RealLLM_MockProject`（**CI 不跑**，本地 + 单独 e2e_real.sh 跑）

### 验证

- ✅ `go test -race ./...` 全过（632 → 660+）
- ✅ E2E 测试（mock LLM）：验证完整事件序列
- ✅ lint: 0 errors
- ✅ CI #99 全绿
- ✅ 手测：mock 项目 → `/api/write/stream?chapter=1` → 看 prompt 含文风 + 角色 + 最近章节

### 工作量：~1.5 周

---

## Sprint 33：SKILL.md 调整（0.5 周）

### 目标
13 个 SKILL.md 全部对齐 Go 端实际能力（V0 实现/简化/未实现标注清楚）。**SKILL.md 是给 LLM 看的提示文档**，必须与 Go 端 API 真实对应。

### 文件清单（13 个改）

| 文件 | 修改内容 |
|------|---------|
| `story.md` | 删 `spawn story-explorer/story-researcher role`（Go 端无）；统一方法名 CamelCase（LoadForWriting 而非 load_for_writing）；删 IDE 集成段 |
| `story-setup.md` | 删 `.claude/.codex/.zcode/.agents` 平台目录段；加"V0 不涉及 IDE 集成目录" |
| `story-review.md` | 4 role 改用 Go 5 role：consistency_checker / chapter_writer / character_extractor / story_outliner；保留"多视角"理念 |
| `browser-cdp.md` | 删 Python 代码示例；加"V0 Go 端未集成 Playwright/chromedp，本 skill 暂不可用，待 P3" |
| `story-long-write.md` | L5 tool call 段标注"V0 Go 端未实现 tool call；P2 Sprint 34 补 OpenAI function calling" |
| `story-long-scan.md` | 标注"references/genre-trends.md 待 Go 端补；V0 暂用 LLM 内置知识" |
| `story-short-scan.md` | 同上 |
| `story-import.md` | 标注"docx 解析待 Go 端补；V0 暂支持 txt + 在线链接" |
| `story-cover.md` | 不动（纯 prompt 模板） |
| `story-deslop.md` | 不动（纯检测规则） |
| `story-long-analyze.md` | 不动（拆文维度 + 输出格式） |
| `story-short-analyze.md` | 不动 |
| `story-short-write.md` | 不动 |

### 关键决策

1. **Role 映射策略**：
   | Python 4 role | Go 替换（覆盖 4 视角） |
   |----------------|-------------------------|
   | consistency-checker | consistency_checker ✅ |
   | narrative-writer | chapter_writer（视角：文风 + 节奏） |
   | character-designer | character_extractor（视角：人物塑造） |
   | story-architect | story_outliner（视角：结构 + 伏笔） |

2. **加 frontmatter `status` 字段**：
   ```yaml
   ---
   name: story-review
   description: "..."
   status: full        # full / partial / missing
   since: v0.30.0
   ---
   ```
   - `full`：Go 端完全实现
   - `partial`：部分实现（标注哪些）
   - `missing`：未实现（Sprint 哪些补）

3. **不破坏现有 frontmatter 结构**：加 `status`/`since` 是向后兼容（YAML 多字段）

### 测试
- 解析 13 个 SKILL.md frontmatter 校验 `status` 字段存在
- 解析 markdown 内容，验证 Go 端提到的方法名都存在（grep 业务代码）

### 验证
- ✅ 13 个文件全部加 frontmatter status
- ✅ Python-specific 行（`async_playwright` / `spawn` / `.claude`）全部清理
- ✅ Role 名与 Go 端 `internal/roles/roles.go` 一一对应

### 工作量：~0.5 周（纯文档改动）

---

## Sprint 34：LLM Tool call + chapter actions 真化（1 周）

### 目标
1. OpenAI / Anthropic / DashScope / DeepSeek 4 provider 都支持 tool call
2. MemoryGraph 4 个方法暴露为 tool（graph_query / foreshadow_query / timeline_query / character_query）
3. chapter_actions.go 5 个 action（expand/insert/rewrite/review/save）全部调真 LLM（替换 mock）

### 文件清单（10 个）

**新增**
- `internal/llm/tools.go`：Tool interface + ToolRequest/ToolResponse struct + `Router.ChatWithTools(ctx, req, tools)`
- `internal/memory/tools.go`：`MemoryGraphTools` 4 个方法包装
- `internal/llm/tools_test.go`：tool call 单元测试（mock provider）
- `internal/memory/tools_test.go`：graph tools 单元测试
- `internal/api/chapter_actions_test.go`：5 个 action 集成测试（mock LLM）

**修改**
- `internal/llm/openai_compat.go`：加 `DoWithTools`（OpenAI format tool calling）
- `internal/llm/anthropic_compat.go`：加 `DoWithTools`（Anthropic format tool calling）
- `internal/llm/dashscope_compat.go`：加 `DoWithTools`（透传 OpenAI format）
- `internal/llm/deepseek_compat.go`：加 `DoWithTools`（透传 OpenAI format）
- `internal/api/chapter_actions.go`：替换 `mockLLMOutput` 路径，加 prompt 模板

### 关键决策

1. **Tool 接口设计**：
   ```go
   type Tool struct {
       Name        string
       Description string
       Parameters  json.RawMessage  // JSON Schema
       Handler     func(ctx context.Context, args json.RawMessage) (ToolResult, error)
   }

   type ToolResult struct {
       Result any
       Error  string  // 非空表示 tool 调用失败
   }

   type ChatWithToolsRequest struct {
       Messages    []Message
       Tools       []Tool
       Model       string
       Temperature float64
       MaxTokens   int
   }
   ```

2. **Provider 适配**：
   - **OpenAI / DashScope / DeepSeek**：`tools` 字段直接传（OpenAI 格式）
   - **Anthropic**：`tools` 字段 + `tool_choice` + 处理 `tool_use` content block
   - 4 provider 共用 `Router.ChatWithTools` 入口 → 内部按 provider 路由

3. **MemoryGraph 4 个 Tool**：
   | Tool | 参数 | 返回 |
   |------|------|------|
   | `graph_query` | `{from: string, to: string, type?: string}` | 路径 + 关系 |
   | `foreshadow_query` | `{chapter?: int, active?: bool}` | 伏笔列表 |
   | `timeline_query` | `{from: int, to: int}` | 时间线事件 |
   | `character_query` | `{name: string, attribute?: string}` | 角色属性 |

4. **chapter_actions 真化 prompt 模板**：
   ```go
   func buildExpandPrompt(outline string, content string, context MemoryContext) string {
       return fmt.Sprintf(`你是 expansion expert。基于以下细纲和已有正文扩写本章。

   [细纲]
   %s

   [已有正文]
   %s

   [角色状态]
   %s

   [文风锚点]
   %s

   要求：
   1. 扩写 500-1000 字
   2. 保持文风一致
   3. 不引入新角色
   4. 返回纯正文（不带标题）`, outline, content, formatCharacters(context), context.Style)
   }
   ```
   5 个 action 各一个 buildPrompt 函数。

5. **chapter_actions post-check**：
   - expand / insert / rewrite 完成后调 `Verifier.PreWriteCheck`（参数：原 outline + 修改后内容）
   - 失败返回 SSE 错误 + 建议修改方向
   - review 直接调 `MultiAgentReviewer.Review`

### 测试

- `internal/llm/tools_test.go`：
  - `TestRouterChatWithTools_OpenAI`（mock response with tool_use）
  - `TestRouterChatWithTools_Anthropic`
  - `TestRouterChatWithTools_DashScope`
  - `TestRouterChatWithTools_DeepSeek`
  - `TestRouterChatWithTools_MultiTurn`（LLM 调 tool → 拿到结果 → 再调 LLM）
- `internal/memory/tools_test.go`：
  - 4 个 tool 各自 mock 测试
- `internal/api/chapter_actions_test.go`：
  - `TestExpandAction_RealLLM`（mock LLM 返回扩写后内容）
  - `TestInsertAction_RealLLM`
  - `TestRewriteAction_RealLLM`
  - `TestReviewAction_MultiAgentReviewer`
  - `TestSaveAction_PersistsFile`

### 验证
- ✅ Tool call 在 4 个 provider 都跑通（mock 测试）
- ✅ chapter_actions 5 个 action 都调真 LLM
- ✅ CI #101 全绿
- ✅ 真 LLM 手测 1 次 expand：mock 项目 → 调 `/api/chapter/5/expand` → 看到 LLM 输出新内容

### 工作量：~1 周

---

## Sprint 35：references/ 目录 + 文风/世界观自动加载（1 周）

### 目标
1. 从 Python V1 端迁 `references/` 全部 .md 文档
2. 实现 `internal/references/` 包：按需加载 + 按 Skill 路由
3. handleStream + chapter_actions 自动调用 references 注入 prompt
4. ProjectStructure 自动读 `设定/` 子目录所有 md 拼到 prompt

### 文件清单（15 个）

**新增**
- `internal/references/loader.go`：References 加载器
- `internal/references/dispatch.go`：根据 SkillName 决定加载哪些 references
- `internal/references/loader_test.go`
- `internal/references/dispatch_test.go`
- `internal/project/load_settings.go`：自动读 设定/ 子目录
- `internal/project/load_settings_test.go`
- `internal/references/assets/大纲排布.md`
- `internal/references/assets/角色设计.md`
- `internal/references/assets/钩子技法.md`
- `internal/references/assets/对话技法.md`
- `internal/references/assets/cover-styles.md`
- `internal/references/assets/genre-trends.md`
- `internal/references/assets/story-craft.md`
- `internal/references/assets/emotion-design.md`
- `internal/references/assets/hook-techniques.md`
- `internal/references/assets/dialogue-craft.md`
- `internal/references/assets/platform-style/` （目录）

**修改**
- `internal/api/write.go`：Sprint 32 基础上调 `refLoader.LoadForSkill(skillName)`
- `internal/api/chapter_actions.go`：同上

### 关键决策

1. **embed.FS 加载**（与 SKILL.md 一致）：
   ```go
   //go:embed assets
   var assetsFS embed.FS
   ```
   启动时一次性加载到内存 map，LoadByName O(1) 查表。

2. **Skill → References 路由表**：
   ```go
   var skillRefs = map[string][]string{
       "story-long-write": {"大纲排布", "角色设计", "钩子技法", "对话技法"},
       "story-short-write": {"story-craft", "emotion-design", "hook-techniques"},
       "story-cover": {"cover-styles"},
       "story-long-scan": {"genre-trends"},
       "story-short-scan": {"genre-trends"},
   }
   ```
   未列出的 skill 返回空切片。

3. **Prompt 拼接顺序（最终版）**：
   ```
   ## 设定
   [ProjectStructure.SetupMD]
   [ProjectStructure.StyleMD]
   [设定/世界观/* 遍历]

   ## 角色状态
   [MemoryContext.L2 per character]

   ## 最近章节
   [MemoryContext.L3 summary list]

   ## 相关事件
   [MemoryContext.L4 top-K]

   ## References
   [refLoader.LoadForSkill(skill)]

   ## 剧情结构
   [references/大纲排布.md]
   ```

4. **token 预算**：total ≤ 8K；超限按反向优先级截（references → L4 → L3 → L2 → L1）。

### 测试
- `internal/references/loader_test.go`：
  - `TestLoadByName`（基本查找）
  - `TestLoadForSkill`（路由）
  - `TestLoadNonExistent`（错误处理）
- `internal/project/load_settings_test.go`：
  - `TestLoadSettingsDir`（读 设定/ 所有 md）
  - `TestLoadWorldviewDir`（读 设定/世界观/）
  - `TestLoadEmpty`（目录不存在）

### 验证
- ✅ 11 个 references .md 全部迁入
- ✅ handleStream 跑长篇时，文风.md 自动注入 prompt
- ✅ CI #103 全绿

### 工作量：~1 周

---

## Sprint 36：E2E 集成测试 + 真实项目验证（0.5 周）

### 目标
跑 2 个真实场景（1 长篇 1 短篇）端到端验证 Go 端**真能跑生产**。

### 文件清单（9 个）

**新增**
- `internal/skills/short_e2e_test.go`：跑 `CompileShortStory` 真调 LLM
- `internal/api/write_e2e_test.go`：跑 `handleStream` 真生成 1 章长篇
- `scripts/e2e_real.sh`：连真实 LLM 跑 1 长篇 + 1 短篇，输出报告
- `scripts/mock_project/`：测试用 mock 项目（设定/ + 大纲/ + _tracking-state.json）
- `.github/workflows/e2e_real.yml`：CI matrix 跑真实 LLM smoke test（optional，手动 trigger）

**修改**
- `.github/workflows/ci.yml`：加 `e2e_smoke` job（mock LLM 测试）
- `README.md`：从"V0.29.4 功能对等 Python"升级为"V1.0.0 production ready"
- `docs/migration-mapping.md`：加 P2 完成度章节
- `docs/p1-completion-plan.md`：加 P2 链接

### 关键决策

1. **E2E 测试 2 套**：
   - **Mock LLM E2E**（CI 默认跑）：用 `httptest.Server` mock provider 响应 → 验证业务逻辑完整
   - **真 LLM E2E**（手动 + CI optional）：连 `DEEPSEEK_API_KEY` / `DASHSCOPE_API_KEY` 跑真实 LLM，验证 prompt 拼接 + 5 层 memory 工作

2. **mock_project 结构**：
   ```
   scripts/mock_project/
   ├── _tracking-state.json     # 5 角色 + 20 章摘要（mock）
   ├── 创作设定.md               # 玄幻 + 热血文风
   ├── 设定/
   │   ├── 文风.md               # 句长 + 标点 + 对话潜台词
   │   └── 世界观/
   │       ├── 力量体系.md
   │       └── 地理.md
   ├── 大纲/
   │   ├── 大纲.md
   │   └── 细纲_第005章.md       # 测试用
   └── 正文/
       └── 第004章.md
   ```

3. **E2E 验证清单**（任一失败 CI 报红）：
   - [ ] handleStream 跑完返回 5+ 章节
   - [ ] SSE event 序列与 SKILL.md 完全一致
   - [ ] 5 层 memory 5 个 section 都注入 prompt
   - [ ] references 自动加载（至少 2 个）
   - [ ] post-write check 通过
   - [ ] _tracking-state.json 被更新
   - [ ] chapter_actions 5 个 action 都成功调用
   - [ ] tool call 在 tool_need 时正常触发 + 回灌
   - [ ] CompileShortStory 跑完 8 节 + deslop 后输出

4. **CI matrix 加 e2e_smoke**：
   ```yaml
   e2e_smoke:
     runs-on: ubuntu-latest
     steps:
       - uses: actions/checkout@v4
       - uses: actions/setup-go@v5
         with: {go-version: '1.23'}
       - run: go test -race -count=1 ./...
       - run: ./scripts/e2e_real.sh  # 用 mock LLM 跑
   ```

5. **README 升级**：
   ```markdown
   # novel2all-go V1.0.0
   
   ## Features (P2 production-ready)
   - ✅ AI 网文长篇生成（5 层 memory + 4 provider + tool call）
   - ✅ AI 短篇生成（8 节 pipeline + 去 AI 味）
   - ✅ 章节 actions（expand/insert/rewrite/review/save）
   - ✅ 多视角审查（4 role 并行）
   - ✅ 一致性 + 文风检查
   - ✅ 完整 SSE 流式 + 取消
   ```

### 验证

- ✅ Mock LLM E2E：2 套测试全过
- ✅ 真 LLM 手测：跑 1 长篇 1 短篇，输出报告
- ✅ CI e2e_smoke job 全过
- ✅ README + docs 同步

### 工作量：~0.5 周

**最终标志**：Sprint 36 完成后 → **`V1.0.0 production-ready`**

---

## 全局验证策略

### 每个 Sprint 必跑
```bash
# 1. 编译
GOTOOLCHAIN=local CGO_ENABLED=1 /c/.../go.exe build ./...

# 2. 测试（race detector）
GOTOOLCHAIN=local CGO_ENABLED=1 /c/.../go.exe test -race -count=1 ./...

# 3. Lint
PATH="/c/.../go/bin:$PATH" /c/.../golangci-lint.exe run --timeout=5m

# 4. gofmt
gofmt -s -l .  # 必须空
```

### CI 必跑（每个 PR + main push）
- 6 个 build（ubuntu/macos/windows × go 1.22/1.23）
- lint（golangci-lint v1.61.0）
- test（go test -race）
- smoke（build binary + start + curl /health）
- e2e_smoke（mock LLM 跑全套 + verify）

### 手动 e2e_real.sh（Sprint 36 后）
```bash
#!/bin/bash
# 跑真实 LLM 端到端
DEEPSEEK_API_KEY=$1 ./scripts/e2e_real.sh
# 输出: data/e2e_report.md (含真 LLM 生成章节 + memory 注入内容)
```

## 风险清单 + 缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| Memory 5 层 token 超 8K | 长篇 prompt 截断 | 优先级截断策略（settings/references → L1/L2/L3） |
| 4 provider tool call 格式不同 | Sprint 34 工作量翻倍 | OpenAI/Anthropic 各 1 个实现；DashScope/DeepSeek 透传 OpenAI |
| 真 LLM 跑不稳定 | E2E 偶发失败 | mock_provider fallback + retry (3 次 + backoff) |
| references/ 文件内容质量 | 注入 prompt 反而误导 | 一次只注入 2-3 个最相关 references |
| chapter_actions 切回 LLM 后速度变慢 | 用户体验下降 | 流式 + 异步 + 客户端 cancel 支持 |
| Memory 状态被并发修改 | Sprint 32 race condition | Tracker 已有 mu；MemoryManager 调 tracker 也持锁 |

## 文件级总览

### 新增（30 个）
```
internal/api/write_e2e_test.go
internal/memory/tools.go
internal/llm/tools.go
internal/llm/tools_test.go
internal/memory/tools_test.go
internal/api/chapter_actions_test.go
internal/references/loader.go
internal/references/dispatch.go
internal/references/loader_test.go
internal/references/dispatch_test.go
internal/references/assets/{11 .md files}
internal/references/assets/platform-style/.gitkeep
internal/project/load_settings.go
internal/project/load_settings_test.go
internal/skills/short_e2e_test.go
scripts/e2e_real.sh
scripts/mock_project/{settings, outline, prose, _tracking-state.json}
.github/workflows/e2e_real.yml
```

### 修改（20+ 个）
```
internal/api/write.go              [Sprint 32: handleStream 全重写]
internal/api/sse.go                [Sprint 32: mock 删除]
internal/api/chapter_actions.go    [Sprint 34: 替换 mock]
internal/memory/types.go           [Sprint 32: ToSystemSections]
internal/server/server.go          [Sprint 32: 注入 memMgr + refLoader]
internal/llm/openai_compat.go      [Sprint 34: DoWithTools]
internal/llm/anthropic_compat.go   [Sprint 34: DoWithTools]
internal/llm/dashscope_compat.go   [Sprint 34: DoWithTools]
internal/llm/deepseek_compat.go    [Sprint 34: DoWithTools]
internal/skills/assets/*.md        [Sprint 33: 13 文件调整]
.github/workflows/ci.yml           [Sprint 36: e2e_smoke]
README.md                          [Sprint 36: 升级 V1.0.0]
docs/migration-mapping.md           [Sprint 36: P2 章节]
docs/p1-completion-plan.md         [Sprint 36: P2 链接]
```

## 总计

- **40+ 个文件**（30 新 + 20+ 改）
- **~4.5 周**（约 200 工时）
- **测试**：632 → 900+ PASS
- **CI**：从 9/9 → 11/11 jobs（+ e2e_smoke + e2e_real optional）
- **生产就绪**：Sprint 36 完成 → V1.0.0

## 决策点（开工前确认）

1. ✅ 是否按 Sprint 顺序执行（32 → 33 + 34 → 35 → 36）？
2. ✅ Sprint 33 是否可以与 32 并行（文档改动独立）？
3. ✅ E2E 测试是否需要 mock + 真 LLM 双跑（推荐）？
4. ✅ 是否允许 Sprint 32 引入 WriteHandler 依赖注入破坏现有签名（推荐加 WithMemory variant）？
5. ✅ chapter_actions 切回 LLM 后是否能接受 5x 慢（mock 立即返回 → LLM 5-15s）？
