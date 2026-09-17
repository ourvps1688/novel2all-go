// Package memory 提供 novel2all-go 长记忆系统.
package memory

// manager.go 实现 MemoryManager 编排层 (Sprint 24).
//
// 设计:
//   - 协调 Tracker + Extractor + Summarizer + Verifier + Graph
//   - 4 个核心 API:
//     1. LoadForWriting(chapter) → MemoryContext  (写前组装)
//     2. UpdateAfterWriting(chapter, content) → 应用提取 + 摘要
//     3. PreWriteCheck(outline) → []ContinuityIssue
//     4. PostWriteCheck(content) → []ContinuityIssue
//
// 参考 Python V0.21+ core/memory/manager.py MemoryManager.
import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// MemoryManager 长记忆系统统一入口.
type MemoryManager struct {
	projectRoot string
	config      MemoryConfig
	llm         *llm.Router
	tracker     *Tracker
	extractor   *Extractor
	summarizer  *ChapterSummarizer
	graph       *MemoryGraph
}

// NewMemoryManager 创建 manager.
//
// projectRoot: 项目根目录 (含 data/_tracking-state.json).
// llm: LLM router (用于 extraction + verification).
func NewMemoryManager(projectRoot string, llmRouter *llm.Router, config MemoryConfig) *MemoryManager {
	if llmRouter == nil {
		// 允许 nil (V0 简化: manager 可仅用 tracker + summarizer)
	}
	return &MemoryManager{
		projectRoot: projectRoot,
		config:      config,
		llm:         llmRouter,
		tracker:     NewTracker(filepath.Join(projectRoot, "data", "_tracking-state.json")),
		extractor:   nil, // lazy: NewExtractor(llm) if llm != nil
		summarizer:  NewChapterSummarizer(),
	}
}

// SetLLM 设置 LLM router (用于 extractor).
//
// 在 cmd/server 拼装层 LLM 创建后调用.
func (m *MemoryManager) SetLLM(router *llm.Router) {
	m.llm = router
	m.extractor = NewExtractor(router)
}

// Tracker 暴露 tracker (供 cmd/server 直接操作).
func (m *MemoryManager) Tracker() *Tracker {
	return m.tracker
}

// Extractor 暴露 extractor (供 post-write 流程使用).
func (m *MemoryManager) Extractor() *Extractor {
	if m.extractor == nil && m.llm != nil {
		m.extractor = NewExtractor(m.llm)
	}
	return m.extractor
}

// Graph 暴露 graph (供读时使用).
func (m *MemoryManager) Graph() *MemoryGraph {
	return m.graph
}

// Config 暴露 config.
func (m *MemoryManager) Config() MemoryConfig {
	return m.config
}

// LoadState 加载或初始化 state.
func (m *MemoryManager) LoadState(projectName string) (*TrackingState, error) {
	state, err := m.tracker.Read()
	if err != nil {
		return nil, fmt.Errorf("read tracker: %w", err)
	}
	if state == nil || (state.ProjectName == "" && projectName != "") {
		// 初始化空 state
		state = newEmptyState(projectName)
		if err := m.tracker.Write(state); err != nil {
			return nil, fmt.Errorf("init tracker: %w", err)
		}
	}
	// 同步 graph
	m.graph = FromState(state)
	return state, nil
}

// LoadForWriting 写作前组装 MemoryContext (V0 简化版).
//
// 实际只填 core + recent + character (后两者目前返回空, 留给 Sprint 25+
// 接入 retriever 后填充).
//
// chapter: 即将写作的章节号.
func (m *MemoryManager) LoadForWriting(ctx context.Context, chapter int) (*MemoryContext, error) {
	state, err := m.LoadState(m.projectName())
	if err != nil {
		return nil, err
	}

	// 1. Core: project settings (V0: 读 创作设定.md / 设定/文风.md)
	core := m.loadCoreSettings()

	// 2. Recent: 最近 N 章摘要 (按 Sprint 22 summarizer bucket)
	recent := m.loadRecentSummaries(state)

	// 3. Character: V0 简化, 返回空 (Sprint 25+ 接入 retriever 后填充)
	// 4. Events: V0 简化, 返回空
	// 5. Graph: V0 简化, 返回空 (Sprint 25+ 整合)

	return &MemoryContext{
		Core:   core,
		Recent: recent,
	}, nil
}

// projectName 返回项目名 (从 _tracking-state.json 读, 没有则用项目根目录 basename).
func (m *MemoryManager) projectName() string {
	state, _ := m.tracker.Read()
	if state != nil && state.ProjectName != "" {
		return state.ProjectName
	}
	return filepath.Base(m.projectRoot)
}

// loadCoreSettings 加载项目核心设定 (创作设定.md + 设定/文风.md).
func (m *MemoryManager) loadCoreSettings() []MemoryItem {
	var items []MemoryItem
	for _, rel := range DefaultSettingFiles {
		fp := filepath.Join(m.projectRoot, rel)
		if data, err := readFile(fp); err == nil {
			items = append(items, MemoryItem{
				Content:    string(data),
				Source:     rel,
				Layer:      LayerCore,
				Relevance:  1.0,
				TokenCount: len(data) / 3,
			})
		}
	}
	return items
}

// loadRecentSummaries 从 state 加载最近 N 章摘要.
func (m *MemoryManager) loadRecentSummaries(state *TrackingState) []MemoryItem {
	summaries := m.tracker.GetRecentSummaries(state, m.config.RecentChapterCount)
	items := make([]MemoryItem, 0, len(summaries))
	for _, s := range summaries {
		items = append(items, MemoryItem{
			Content:    fmt.Sprintf("第%d章: %s", s.Chapter, s.Summary),
			Source:     fmt.Sprintf("chapter:%d", s.Chapter),
			Layer:      LayerRecent,
			Relevance:  1.0,
			TokenCount: len(s.Summary) / 3,
		})
	}
	return items
}

// UpdateAfterWriting 写完后更新 tracking (extract + merge + save).
//
// chapter: 已写章节号.
// content: 章节正文.
//
// 流程:
//  1. 调 LLM 提取 (ExtractedChapterInfo)
//  2. 应用到 state (character/foreshadowing/timeline/summary/graph)
//  3. 持久化 state
func (m *MemoryManager) UpdateAfterWriting(ctx context.Context, chapter int, content string) (*TrackingState, error) {
	state, err := m.LoadState(m.projectName())
	if err != nil {
		return nil, err
	}

	// 1. Extract (如 llm 不可用, 跳过)
	ext := m.Extractor()
	if ext != nil {
		extracted, err := ext.Extract(ctx, chapter, content, state)
		if err == nil && extracted != nil {
			state = ext.ApplyToState(state, extracted)
		}
		// 失败兜底: 仍继续 (写 summary + save)
	}

	// 2. Apply summary (extractive, 不依赖 LLM)
	state = m.summarizer.ApplyToState(state, chapter, content)

	// 3. Save
	if err := m.tracker.Write(state); err != nil {
		return nil, fmt.Errorf("write state: %w", err)
	}

	// 4. 同步 graph
	m.graph = FromState(state)
	return state, nil
}

// PreWriteCheck 写前一致性检查 (V0 简化: 返回空, 等 Sprint 24+ 接 LLM).
//
// outline: 本章大纲 (string).
func (m *MemoryManager) PreWriteCheck(ctx context.Context, outline string) ([]ContinuityIssue, error) {
	// V0: 暂不调 LLM (避免增加测试复杂度, Sprint 25 接 pre-write prompt)
	// 注: Sprint 24 + Sprint 25 任务边界, 这里留 stub
	return []ContinuityIssue{}, nil
}

// PostWriteCheck 写后一致性检查 (V0 简化: 返回空).
//
// content: 章节正文.
func (m *MemoryManager) PostWriteCheck(ctx context.Context, content string) ([]ContinuityIssue, error) {
	// V0 stub
	return []ContinuityIssue{}, nil
}

// readFile 简单 wrapper (便于测试 mock).
func readFile(fp string) ([]byte, error) {
	return readFileOS(fp)
}
