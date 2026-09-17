// Package testfixtures 提供 E2E 测试共享的 fixture (Sprint 36).
//
// 设计目标:
//   - 一个 mock_project fixture 复用所有 E2E test (CompileShortStory / handleStream / chapter actions)
//   - mock LLM executor 注入可预测的输出 (无需真实 LLM API key)
//   - 真实项目结构 (设定/ + 大纲/ + 正文/) 模拟 V0.30 用户场景
//
// 不依赖: 真实 LLM API, 真实文件 IO 之外的资源.
package testfixtures

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// MockExecutor mock LLM executor for E2E tests (Sprint 36).
//
// 按 skill 名返回预置 response. 如果 skill 不在预设中, 返回默认 mock 输出.
// 每次 Execute/ExecuteStream 都会记录到 CallLog (按调用顺序).
type MockExecutor struct {
	mu sync.Mutex

	// Responses skill 名 → mock 输出. 未设置用 DefaultOutput.
	Responses map[string]string

	// DefaultOutput 未匹配 skill 时的默认输出.
	DefaultOutput string

	// CallLog 每次 Execute 的调用记录 [{SkillName, UserInput, Timestamp}].
	CallLog []MockCall
}

// MockCall 单次 LLM 调用的记录.
type MockCall struct {
	SkillName   string
	UserInput   string
	SystemInput string
	Vars        map[string]any
}

// NewMockExecutor 构造 (默认输出: "mock output for {skill}").
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		Responses:     make(map[string]string),
		DefaultOutput: "这是 mock 输出, 测试专用.",
	}
}

// SetResponse 设置 skill 的预置输出.
func (m *MockExecutor) SetResponse(skill, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses[skill] = output
}

// Execute 模拟 LLM 调用.
func (m *MockExecutor) Execute(ctx context.Context, input skills.ExecuteInput) (*skills.ExecuteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallLog = append(m.CallLog, MockCall{
		SkillName:   input.SkillName,
		UserInput:   input.UserInput,
		SystemInput: input.SystemInput,
		Vars:        input.Variables,
	})
	out, ok := m.Responses[input.SkillName]
	if !ok {
		out = fmt.Sprintf("%s\n[skill=%s, user_input=%s]", m.DefaultOutput, input.SkillName, input.UserInput)
	}
	return &skills.ExecuteResult{
		Content:   out,
		TokensIn:  100,
		TokensOut: len(out),
	}, nil
}

// ExecuteStream 模拟流式调用 (Sprint 36: pipeline.Run 用流式).
//
// 实现要点:
//   - 同步发完所有 chunk 后 caller 负责 close channel
//   - 但 caller 是 (api) sendExecuteStream goroutine, 它不 close!
//   - 所以这里用 type assertion 把 chan<- 转成 chan (只发送端 close 需要 send-only 类型).
//
// 实际写法: 用 recover + 在 close 前做 ctx check. Sprint 36 简化: 直接 close channel.
func (m *MockExecutor) ExecuteStream(ctx context.Context, input skills.ExecuteInput, ch chan<- llm.Chunk) error {
	res, err := m.Execute(ctx, input)
	if err != nil {
		ch <- llm.Chunk{Err: err}
		return err
	}
	ch <- llm.Chunk{Content: res.Content}
	// Caller (api.handleStream sendExecuteStream goroutine) 必须 close channel.
	// 修 handleStream 内的 sendExecuteStream goroutine 加 defer close(ch).
	return nil
}

// CallCount 返回 mock executor 被调用次数.
func (m *MockExecutor) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.CallLog)
}

// LastCall 返回最后一次调用 (用于断言).
func (m *MockExecutor) LastCall() *MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.CallLog) == 0 {
		return nil
	}
	c := m.CallLog[len(m.CallLog)-1]
	return &c
}

// SetupMockProject 创建完整 mock 项目目录 (Sprint 36 E2E fixture).
//
// 结构:
//
//	{dir}/
//	设定/文风.md              // 文风 (V0.30 必读)
//	设定/创作设定.md          // 总设定 (可读)
//	设定/世界观/地图.md       // 子目录遍历测试
//	大纲/总纲.md              // 主线大纲
//	大纲/细纲_第001章.md      // 章纲
//	正文/第001章.md           // 章节正文
//	_tracking-state.json       // memory 跟踪状态
//
// 返回 dir 路径 (用 t.TempDir() 创建, 自动清理).
func SetupMockProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	dirs := []string{
		filepath.Join(dir, "设定"),
		filepath.Join(dir, "设定", "世界观"),
		filepath.Join(dir, "大纲"),
		filepath.Join(dir, "正文"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	files := map[string]string{
		"设定/文风.md":       "# 文风\n\n第一人称, 短句为主, 避免翻译腔.\n",
		"设定/创作设定.md":     "# 创作设定\n\n类型: 玄幻\n风格: 升级流\n主角: 林雷\n",
		"设定/世界观/地图.md":   "# 地图\n\n九州大陆, 北荒 + 中原 + 南海.\n",
		"大纲/总纲.md":       "# 总纲\n\n林雷从杂役弟子成长为九州之主, 中间经历 5 个大阶段.\n",
		"大纲/细纲_第001章.md": "# 第 1 章 细纲\n\n林雷被师尊逐出山门, 意外获得禁忌功法.\n- 钩子: 师尊拔剑瞬间\n- 冲突: 同门师弟师妹的嘲讽\n- 转折: 林雷觉醒血脉\n",
		"正文/第001章.md":    "# 第 1 章\n\n[AI 草稿] 林雷被师尊逐出山门.\n",
	}
	for rel, body := range files {
		fp := filepath.Join(dir, rel)
		if err := os.WriteFile(fp, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", fp, err)
		}
	}
	return dir
}
