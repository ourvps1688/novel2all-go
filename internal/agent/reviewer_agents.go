package agent

// ReviewerAgent 单个审稿 agent（Sprint A5.7 ReviewerAgentSet 的一部分）
//
// 当前为 stub — 完整 agent framework 接入待 Sprint A5.7 后续实现。
// 现有 MultiAgentReviewer.ReviewWithAgents() 已接受 ReviewerAgentSet 参数。
type ReviewerAgent struct {
	// Role 审稿角色名（story_outliner / chapter_writer / 等）
	Role string
}

// ReviewerAgentSet 4 个审稿 agent 的集合 (Sprint A5.7)
//
// 包含 4 个 vendor reviewer role：
//   - story_outliner (story-architect alias)
//   - chapter_writer (narrative-writer alias)
//   - consistency_checker
//   - story_reviewer (character-designer alias)
//
// 完整实现路线：
//   - Sprint A5.7 完整化 ReviewWithAgents() 用 RunAgent + Orchestrator
//   - 4 个 reviewer 用 A5.4 Orchestrator.RunParallel 并行跑
//   - 每个 reviewer 用 agent.Run() 走统一框架
type ReviewerAgentSet struct {
	// Reviewers role alias → agent
	Reviewers map[string]*ReviewerAgent
}
