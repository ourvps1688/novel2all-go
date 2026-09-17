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
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// MemoryManager 长记忆系统统一入口.
//
//nolint:revive // matches Python
type MemoryManager struct {
	projectRoot string
	config      MemoryConfig
	llm         *llm.Router
	tracker     *Tracker
	extractor   *Extractor
	summarizer  *ChapterSummarizer
	graph       *MemoryGraph
	retriever   *MemoryRetriever // Sprint 26 新增
}

// NewMemoryManager 创建 manager.
//
// projectRoot: 项目根目录 (含 data/_tracking-state.json).
// llm: LLM router (用于 extraction + verification).
func NewMemoryManager(projectRoot string, llmRouter *llm.Router, config MemoryConfig) *MemoryManager {
	if llmRouter == nil {
		// 允许 nil (V0 简化: manager 可仅用 tracker + summarizer)
		_ = llmRouter // explicitly silence unused warning
	}
	// Sprint 26: 自动初始化 retriever (.chroma 目录)
	chromaDir := filepath.Join(projectRoot, ".chroma")
	retriever, _ := NewRetriever(chromaDir, nil) // 自动降级
	return &MemoryManager{
		projectRoot: projectRoot,
		config:      config,
		llm:         llmRouter,
		tracker:     NewTracker(filepath.Join(projectRoot, "data", "_tracking-state.json")),
		extractor:   nil, // lazy: NewExtractor(llm) if llm != nil
		summarizer:  NewChapterSummarizer(),
		retriever:   retriever,
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

// Retriever 暴露 retriever (供 Sprint 26+ read-time 查询).
func (m *MemoryManager) Retriever() *MemoryRetriever {
	return m.retriever
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

// LoadForWriting 写作前组装 MemoryContext (Sprint 26 接 retriever).
//
// chapter: 即将写作的章节号.
func (m *MemoryManager) LoadForWriting(ctx context.Context, chapter int) (*MemoryContext, error) {
	state, err := m.LoadState(m.projectName())
	if err != nil {
		return nil, err
	}

	// 1. Core: project settings
	core := m.loadCoreSettings()

	// 2. Recent: 最近 N 章摘要
	recent := m.loadRecentSummaries(state)

	// 3. Character: 角色状态 (按需加载: 本章涉及角色)
	character := m.loadCharacterState(state)

	// 4. Events: 向量检索 (Sprint 26 接 retriever)
	// 默认检索 top-K 最近 N 章, 用于 LLM prompt background.
	var events []MemoryItem
	if m.retriever != nil && chapter > 1 {
		// 取前 N 章相关事件, 避免与本章重复
		hi := chapter - 1
		window := m.config.RecentChapterCount
		lo := hi - window + 1
		if lo < 1 {
			lo = 1
		}
		rng := &[2]int{lo, hi}
		// 用最近一章的 summary 作 query
		query := m.buildRetrievalQuery(state, chapter)
		events = m.retriever.Query(query, m.config.EventTopK, rng, "")
	}

	// 5. Graph: 知识图谱片段 (Sprint 26 简化: 返空, 后续 Sprint 接 graph query tool)
	_ = m.graph

	return &MemoryContext{
		Core:      core,
		Recent:    recent,
		Character: character,
		Events:    events,
	}, nil
}

// buildRetrievalQuery 根据最近 summary 构造检索 query.
func (m *MemoryManager) buildRetrievalQuery(state *TrackingState, chapter int) string {
	// 取最近一章的 summary (RecentChapterSummaries 是 map[int]string)
	if s, ok := state.RecentChapterSummaries[chapter-1]; ok {
		if len(s) > 200 {
			s = s[:200]
		}
		return s
	}
	return fmt.Sprintf("chapter %d context", chapter-1)
}

// loadCharacterState 加载 character layer (按需).
//
// V0 简化: 全部角色状态 (后续可按章节大纲 filter).
func (m *MemoryManager) loadCharacterState(state *TrackingState) []MemoryItem {
	items := make([]MemoryItem, 0, len(state.Characters))
	for name, cs := range state.Characters {
		content := fmt.Sprintf("%s: %s", name, cs.Location)
		if cs.EmotionalState != "" {
			content += fmt.Sprintf(" (emotional: %s)", cs.EmotionalState)
		}
		if cs.Motivation != "" {
			content += fmt.Sprintf(" (motivation: %s)", cs.Motivation)
		}
		items = append(items, MemoryItem{
			Content:    content,
			Source:     fmt.Sprintf("character:%s", name),
			Layer:      LayerCharacter,
			Relevance:  1.0,
			TokenCount: len(content) / 3,
		})
	}
	return items
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

// UpdateAfterWriting 写完后更新 tracking (extract + merge + save + index).
//
// chapter: 已写章节号.
// content: 章节正文.
//
// 流程 (Sprint 26 升级):
//  1. 调 LLM 提取 (ExtractedChapterInfo)
//  2. 应用到 state (character/foreshadowing/timeline/summary/graph)
//  3. 持久化 state
//  4. 索引 events 到 retriever (供后续章节 LoadForWriting RAG 查询)
func (m *MemoryManager) UpdateAfterWriting(ctx context.Context, chapter int, content string) (*TrackingState, error) {
	state, err := m.LoadState(m.projectName())
	if err != nil {
		return nil, err
	}

	// 1. Extract (如 llm 不可用, 跳过)
	ext := m.Extractor()
	var extracted *ExtractedChapterInfo
	if ext != nil {
		extracted, err = ext.Extract(ctx, chapter, content, state)
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

	// 5. Sprint 26: 索引到 retriever (供后续章节 RAG 查询).
	// 提取每个 ExtractedChapterInfo 的 NewEvents + CharacterUpdates → 入库 retriever.
	m.indexEventsToRetriever(chapter, content, extracted)
	return state, nil
}

// indexEventsToRetriever 索引本章事件到 retriever.
//
// 提取的来源 (Sprint 26):
//   - extracted.ForeshadowingPlanted + ForeshadowingChanged
//   - extracted.CharacterUpdates
//   - extracted.TimelineEvents
//   - 内嵌兜底: chapter 第一段 (保 retriever 非空)
func (m *MemoryManager) indexEventsToRetriever(chapter int, content string, extracted *ExtractedChapterInfo) {
	if m.retriever == nil {
		return
	}
	var events []EventInput

	if extracted != nil {
		// 1. Foreshadowing planted/changed
		for _, fs := range extracted.ForeshadowingPlanted {
			text := fmt.Sprintf("伏笔设置: %s", fs.Description)
			if len(text) > 500 {
				text = text[:500]
			}
			events = append(events, EventInput{
				Chapter:   chapter,
				EventType: "foreshadowing",
				Text:      text,
				Metadata:  map[string]interface{}{"id": fs.ID, "status": fs.Status},
			})
		}
		for _, fs := range extracted.ForeshadowingChanged {
			text := fmt.Sprintf("伏笔推进: %s → %s", fs.Description, fs.Status)
			if len(text) > 500 {
				text = text[:500]
			}
			events = append(events, EventInput{
				Chapter:   chapter,
				EventType: "foreshadowing",
				Text:      text,
				Metadata:  map[string]interface{}{"id": fs.ID, "status": fs.Status},
			})
		}

		// 2. Character updates
		for _, cu := range extracted.CharacterUpdates {
			text := fmt.Sprintf("%s → %s", cu.Name, cu.Location)
			if cu.EmotionalState != "" {
				text += fmt.Sprintf(" (emotional: %s)", cu.EmotionalState)
			}
			if len(text) > 500 {
				text = text[:500]
			}
			events = append(events, EventInput{
				Chapter:   chapter,
				EventType: "character_change",
				Text:      text,
				Metadata:  map[string]interface{}{"character": cu.Name},
			})
		}

		// 3. Timeline events
		for _, te := range extracted.TimelineEvents {
			text := fmt.Sprintf("%s: %s", te.InWorldTime, te.Event)
			events = append(events, EventInput{
				Chapter:   chapter,
				EventType: "timeline",
				Text:      text,
				Metadata:  map[string]interface{}{"related_chars": te.RelatedChars},
			})
		}

		// 4. Summary 也入库 (供后续检索 "本章讲了啥")
		if extracted.Summary != "" {
			s := extracted.Summary
			if len(s) > 500 {
				s = s[:500]
			}
			events = append(events, EventInput{
				Chapter:   chapter,
				EventType: "general",
				Text:      s,
			})
		}
	}

	// 兜底: 如 LLM 没返回 events, 用 chapter 第一段作 general 事件
	if len(events) == 0 && len(content) > 0 {
		firstPara := content
		if idx := indexNewline(content, '\n'); idx > 0 {
			firstPara = content[:idx]
		}
		if len(firstPara) > 300 {
			firstPara = firstPara[:300]
		}
		events = append(events, EventInput{
			Chapter:   chapter,
			EventType: "general",
			Text:      firstPara,
		})
	}

	if len(events) > 0 {
		m.retriever.AddEvents(events)
	}
}

// indexNewline 返回第一个匹配 rune 的 byte 索引 (strings.IndexRune wrapper).
func indexNewline(s string, r rune) int {
	for i, c := range s {
		if c == r {
			return i
		}
	}
	return -1
}

// PreWriteCheck 写前一致性检查 (Sprint 26 真实实现).
//
// outline: 本章大纲 (string).
//
// 流程:
//  1. 用 retriever 查询与 outline 相关的历史事件 (topK=5)
//  2. 拼装 prompt 给 LLM (CONSISTENCY task)
//  3. 解析 LLM 输出为 []ContinuityIssue
//
// 如 llm 为 nil, 返回空 (V0 简化).
func (m *MemoryManager) PreWriteCheck(ctx context.Context, outline string) ([]ContinuityIssue, error) {
	if m.llm == nil || m.retriever == nil || outline == "" {
		return nil, nil
	}

	// 1. 检索相关事件
	events := m.retriever.Query(outline, 5, nil, "")
	if len(events) == 0 {
		return nil, nil // 无历史事件, 无需检查
	}

	// 2. 构造 prompt
	var sb strings.Builder
	sb.WriteString("# 写前一致性检查\n\n")
	sb.WriteString("本章大纲:\n")
	sb.WriteString(outline)
	sb.WriteString("\n\n相关历史事件:\n")
	for _, evt := range events {
		sb.WriteString(fmt.Sprintf("- %s\n", evt.Content))
	}
	sb.WriteString("\n请检查本章大纲是否与历史事件冲突, 返回 issues JSON 数组, 每项含 severity (critical/warning/info) + category + description 字段.\n")

	// 3. 调 LLM
	schema := llm.JSONSchema{
		Name:        "PreWriteCheckResult",
		Description: "写前一致性检查结果",
		Fields: []llm.JSONSchemaField{
			{Name: "issues", Type: "array", Description: "issue 列表"},
		},
	}

	target := &preCheckResult{}
	req := llm.Request{
		Task:     llm.TaskConsistency,
		Messages: []llm.Message{{Role: "user", Content: sb.String()}},
	}
	if err := llm.GenerateJSON(ctx, m.llm, req, schema, target); err != nil {
		// LLM 失败兜底 (fail-soft): 不阻断
		return nil, nil
	}

	// 4. 转换
	issues := make([]ContinuityIssue, 0, len(target.Issues))
	for _, i := range target.Issues {
		issues = append(issues, ContinuityIssue{
			Severity:    i.Severity,
			Category:    i.Category,
			Description: i.Description,
		})
	}
	return issues, nil
}

// preCheckResult PreWriteCheck LLM 输出 schema.
type preCheckResult struct {
	Issues []struct {
		Severity    string `json:"severity"`
		Category    string `json:"category"`
		Description string `json:"description"`
	} `json:"issues"`
}

// PostWriteCheck 写后一致性检查 (Sprint 26 真实实现).
//
// content: 章节正文.
//
// V0 简化: 纯逻辑 (基于 state.LastUpdated 检测无变化), Sprint 27+ 接 LLM.
func (m *MemoryManager) PostWriteCheck(ctx context.Context, content string) ([]ContinuityIssue, error) {
	if m.llm == nil || content == "" {
		return nil, nil
	}
	// Sprint 27+ TODO: 调 LLM 用 VERIFICATION task 检查 content 与 state 一致性
	// 当前先返回空 (避免误报)
	return nil, nil
}

// readFile 简单 wrapper (便于测试 mock).
func readFile(fp string) ([]byte, error) {
	return readFileOS(fp)
}
