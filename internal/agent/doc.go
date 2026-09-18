// Package agent 实现 vendor oh-story-dsh-0.1.9 的 Agent framework。
//
// Sprint A1.1-A1.7 (V2.0.0) — 100% 对齐 vendor 的 7 roles + 6 tools + agent framework。
//
// 设计目标：
//   - 复用 Go 端现有 llm.Router（4 provider 统一 Anthropic 兼容协议）
//   - 复用现有 memory.MemoryManager（5 层 memory）
//   - 复用现有 skills.Loader（13 SKILL.md + 242 references）
//   - 仅添加 agent orchestration 层，不破坏现有 17 packages
//
// 关键类型：
//   - AgentSpec:  解析 vendor role .md 的 frontmatter + body 后的规格
//   - Agent:      运行时实例（含 LLM + tools + memory + state）
//   - Registry:   按 role name 查 Agent 的注册表
//
// 本包 Sprint A1 阶段仅实现：
//   - AgentSpec 数据结构
//   - frontmatter YAML 解析（minimal parser, 无外部依赖）
//   - role .md 文件加载
//   - LoadAgent() + Registry
//   - 基础单元测试
//
// Sprint A5 才实现 Agent.Run() 主循环（maxTurns + tool call + state）。
package agent
