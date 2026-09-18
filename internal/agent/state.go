package agent

import (
	"sync"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// AgentState Agent 运行期状态 (Sprint A5.3)
//
//nolint:revive // stutter: agent.State 字段都是 agent 上下文，但 State 是常用名
type AgentState struct {
	mu sync.Mutex

	// Messages LLM 对话历史（system + user + assistant + tool results）
	Messages []llm.Message

	// Turn 当前轮次（0-based，已 successful turn）
	Turn int

	// Tools 此 agent 注入的工具（来自 tools.Registry，按 vendor model 决定）
	Tools []string // tool name 列表（"Read" / "Write" 等）

	// ProjectRoot 项目根目录（文件工具 sandbox 根）
	ProjectRoot string
}

// NewAgentState 创建新 state（每次 Run() 调用一次）
func NewAgentState(projectRoot string) *AgentState {
	return &AgentState{
		ProjectRoot: projectRoot,
	}
}

// AppendMessage 加消息（线程安全）
func (s *AgentState) AppendMessage(m llm.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, m)
}

// MessagesSnapshot 返回当前消息快照（避免外部修改）
func (s *AgentState) MessagesSnapshot() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]llm.Message, len(s.Messages))
	copy(out, s.Messages)
	return out
}

// ResetMessages 重置（保留 system message）
func (s *AgentState) ResetMessages(keepSystem bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !keepSystem || len(s.Messages) == 0 {
		s.Messages = nil
		return
	}
	if s.Messages[0].Role == "system" {
		s.Messages = s.Messages[:1]
	} else {
		s.Messages = nil
	}
}
