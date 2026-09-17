// Package memory 提供 novel2all-go 长记忆系统.
package memory

// extractor.go 实现 LLM-driven 结构化信息提取 (Sprint 23).
//
// 设计:
//   - Extractor 用 llm.Router 调用 LLM (TaskType.EXTRACTION)
//   - 用 instructor 模式 (llm.GenerateJSON) 获取 JSON 输出
//   - JSON schema → ExtractedChapterInfo struct
//   - ApplyToState 把提取结果 merge 到 TrackingState
//
// 参考 Python V0.23+ core/memory/extractor.py Extractor.
import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// CharacterUpdate 角色状态变化 (extractor 输出).
type CharacterUpdate struct {
	Name                 string   `json:"name"`
	Location             string   `json:"location,omitempty"`
	EmotionalState       string   `json:"emotional_state,omitempty"`
	Motivation           string   `json:"motivation,omitempty"`
	KnowledgeAdded       []string `json:"knowledge_added"`
	RelationshipsChanged []string `json:"relationships_changed,omitempty"`
}

// ForeshadowingUpdate 伏笔状态变化 (extractor 输出).
type ForeshadowingUpdate struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"` // active / advanced / revealed / abandoned
	Notes       string `json:"notes,omitempty"`
}

// TimelineEventUpdate 时间线事件 (extractor 输出).
type TimelineEventUpdate struct {
	InWorldTime  string   `json:"in_world_time"`
	Event        string   `json:"event"`
	RelatedChars []string `json:"related_chars,omitempty"`
}

// ExtractedGraphNode 图谱节点增量 (extractor 输出).
type ExtractedGraphNode struct {
	ID          string   `json:"id"`
	Type        NodeType `json:"type"`
	Name        string   `json:"name"`
	Alive       *bool    `json:"alive,omitempty"`
	TypeDetail  *string  `json:"type_detail,omitempty"`
	IDShort     *string  `json:"id_short,omitempty"`
	Status      *string  `json:"status,omitempty"`
	InWorldTime *string  `json:"in_world_time,omitempty"`
}

// ExtractedGraphEdge 图谱边增量 (extractor 输出).
type ExtractedGraphEdge struct {
	FromID string   `json:"from_id"`
	ToID   string   `json:"to_id"`
	Type   EdgeType `json:"type"`
	Label  string   `json:"label,omitempty"`
}

// ExtractedGraphData 图谱增量 (extractor 输出).
type ExtractedGraphData struct {
	Nodes []ExtractedGraphNode `json:"nodes"`
	Edges []ExtractedGraphEdge `json:"edges"`
}

// ExtractedChapterInfo LLM 提取的章节信息 (instructor 输出 schema).
type ExtractedChapterInfo struct {
	Chapter              int                   `json:"chapter"`
	CharacterUpdates     []CharacterUpdate     `json:"character_updates"`
	ForeshadowingPlanted []ForeshadowingUpdate `json:"foreshadowing_planted"`
	ForeshadowingChanged []ForeshadowingUpdate `json:"foreshadowing_changed"`
	TimelineEvents       []TimelineEventUpdate `json:"timeline_events"`
	Summary              string                `json:"summary"`
	ContinuityIssues     []ContinuityIssue     `json:"continuity_issues"`
	Graph                ExtractedGraphData    `json:"graph"`
}

// Extractor 从章节正文提取结构化信息.
type Extractor struct {
	router *llm.Router
}

// NewExtractor 创建.
func NewExtractor(router *llm.Router) *Extractor {
	return &Extractor{router: router}
}

// ExtractionPrompt 提取 prompt (对齐 Python V0.23+ EXTRACTION_PROMPT).
const ExtractionPrompt = `你是小说连续性审计员。分析本章正文，提取结构化信息。

## 已有状态（参考）
{previous_state}

## 本章正文
{content}

## 任务
返回严格 JSON，包含以下字段：
- chapter: 本章编号
- character_updates: 角色状态变化列表
- foreshadowing_planted: 本章新埋下的伏笔
- foreshadowing_changed: 本章状态发生变化的伏笔
- timeline_events: 本章时间线事件
- summary: 200 字内本章摘要
- continuity_issues: 你发现的任何连续性问题
- graph.nodes: 本章新出现的图谱节点
- graph.edges: 本章新出现的关系边

注意：
- 只记录本章确实出现或发生关系变化的内容
- id 格式: "类型:名称"
- continuity_issues.severity: critical / warning / info

只返回 JSON，不要其他文字。`

// Extract 提取章节信息 (LLM 调用).
//
// chapter: 本章编号
// content: 本章正文
// previousState: 已有 state (含 characters/foreshadowing/timeline/graph)
//
// 返回 ExtractedChapterInfo. 失败时返回含 warning 的空结果 (不阻断主流程).
func (e *Extractor) Extract(ctx context.Context, chapter int, content string, previousState *TrackingState) (*ExtractedChapterInfo, error) {
	previousStateDict := stateToPromptDict(previousState)
	previousJSON, _ := json.Marshal(previousStateDict)

	prompt := replacePlaceholder(ExtractionPrompt, "{previous_state}", string(previousJSON))
	prompt = replacePlaceholder(prompt, "{content}", content)

	// 调 LLM (instructor 模式 → JSON)
	schema := llm.JSONSchema{
		Name:        "ExtractedChapterInfo",
		Description: "从章节正文提取的结构化信息",
		Fields: []llm.JSONSchemaField{
			{Name: "chapter", Type: "int", Required: true},
			{Name: "summary", Type: "string", Required: false},
			{Name: "character_updates", Type: "array", Items: "CharacterUpdate"},
			{Name: "foreshadowing_planted", Type: "array", Items: "ForeshadowingUpdate"},
			{Name: "foreshadowing_changed", Type: "array", Items: "ForeshadowingUpdate"},
			{Name: "timeline_events", Type: "array", Items: "TimelineEventUpdate"},
			{Name: "continuity_issues", Type: "array", Items: "ContinuityIssue"},
			{Name: "graph", Type: "object", Items: "ExtractedGraphData"},
		},
	}
	result := &ExtractedChapterInfo{}
	req := llm.Request{
		Task: llm.TaskExtraction,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	}
	if err := llm.GenerateJSON(ctx, e.router, req, schema, result); err != nil {
		// 失败兜底: 返回 warning (Python V0.23+ 行为)
		empty := &ExtractedChapterInfo{
			Chapter: chapter,
			ContinuityIssues: []ContinuityIssue{{
				Severity:    "warning",
				Category:    "setting",
				Description: fmt.Sprintf("LLM extraction failed: %v", err),
			}},
		}
		return empty, fmt.Errorf("extract: %w", err)
	}
	// 确保 chapter 是传入值 (防止 LLM 误判)
	result.Chapter = chapter
	return result, nil
}

// ApplyToState 把 extracted 应用到 state (精细 merge).
//
// 1. character_updates: 更新已有角色或新增
// 2. foreshadowing_planted: 新增伏笔
// 3. foreshadowing_changed: 更新伏笔状态
// 4. timeline_events: 追加到 timeline (按 chapter 排序)
// 5. summary: 加入 recent_chapter_summaries
// 6. graph: merge 到 state.Graph (用 MemoryGraph merge)
func (e *Extractor) ApplyToState(state *TrackingState, extracted *ExtractedChapterInfo) *TrackingState {
	if extracted == nil {
		return state
	}

	// 1. Characters
	for _, cu := range extracted.CharacterUpdates {
		existing := state.Characters[cu.Name]
		merged := CharacterState{
			Name:               cu.Name,
			Location:           pickStr(cu.Location, existing.Location),
			EmotionalState:     pickStr(cu.EmotionalState, existing.EmotionalState),
			Motivation:         pickStr(cu.Motivation, existing.Motivation),
			Knowledge:          mergeStrSlice(existing.Knowledge, cu.KnowledgeAdded),
			LastUpdatedChapter: extracted.Chapter,
		}
		state.Characters[cu.Name] = merged
	}

	// 2. Foreshadowing planted
	for _, fs := range extracted.ForeshadowingPlanted {
		state.Foreshadowing[fs.ID] = ForeshadowingState{
			ID:             fs.ID,
			Description:    fs.Description,
			PlantedChapter: extracted.Chapter,
			Status:         pickStr(fs.Status, "active"),
			Notes:          fs.Notes,
		}
	}

	// 3. Foreshadowing changed
	for _, fs := range extracted.ForeshadowingChanged {
		existing, ok := state.Foreshadowing[fs.ID]
		if !ok {
			existing = ForeshadowingState{ID: fs.ID, PlantedChapter: extracted.Chapter, Status: "active"}
		}
		if fs.Status != "" {
			existing.Status = fs.Status
		}
		if fs.Description != "" {
			existing.Description = fs.Description
		}
		if fs.Notes != "" {
			existing.Notes = fs.Notes
		}
		state.Foreshadowing[fs.ID] = existing
	}

	// 4. Timeline events (按 chapter 追加)
	for _, te := range extracted.TimelineEvents {
		state.Timeline = append(state.Timeline, TimelineEvent{
			Chapter:      extracted.Chapter,
			InWorldTime:  te.InWorldTime,
			Event:        te.Event,
			RelatedChars: te.RelatedChars,
		})
	}

	// 5. Summary → recent_chapter_summaries
	if extracted.Summary != "" {
		state.RecentChapterSummaries[extracted.Chapter] = extracted.Summary
	}

	// 6. Graph merge
	if state.Graph == nil {
		state.Graph = &GraphData{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	}
	mg := NewMemoryGraph(state.Graph)
	for _, n := range extracted.Graph.Nodes {
		mg.AddNode(GraphNode{
			ID:           n.ID,
			Type:         n.Type,
			Name:         n.Name,
			Alive:        n.Alive,
			TypeDetail:   n.TypeDetail,
			IDShort:      n.IDShort,
			Status:       n.Status,
			InWorldTime:  n.InWorldTime,
			FirstChapter: intPtr(extracted.Chapter),
		})
	}
	for _, e := range extracted.Graph.Edges {
		mg.AddEdge(GraphEdge{
			FromID: e.FromID,
			ToID:   e.ToID,
			Type:   e.Type,
		})
	}
	// 更新 LastUpdatedChapter
	state.LastUpdatedChapter = extracted.Chapter

	return state
}

// intPtr helper.
func intPtr(i int) *int { return &i }

// stateToPromptDict state → dict for prompt (Python dict() 兼容).
func stateToPromptDict(state *TrackingState) map[string]any {
	if state == nil {
		return map[string]any{}
	}
	return map[string]any{
		"project_name":  state.ProjectName,
		"characters":    state.Characters,
		"foreshadowing": state.Foreshadowing,
		"timeline":      state.Timeline,
		"graph":         state.Graph,
		"last_chapter":  state.LastUpdatedChapter,
	}
}

// pickStr 返回 first 非空值 (merge helper).
func pickStr(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

// mergeStrSlice 合并两个 string slice (去重).
func mergeStrSlice(a, b []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// replacePlaceholder 简单占位符替换 (避免 import strings).
func replacePlaceholder(s, old, new string) string {
	// strings.Replace all occurrences
	for {
		idx := indexOf(s, old)
		if idx < 0 {
			return s
		}
		s = s[:idx] + new + s[idx+len(old):]
	}
}

// indexOf 简单子串查找 (避免 import strings).
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
