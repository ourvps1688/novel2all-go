// Package memory 提供 novel2all-go 长记忆系统.
package memory

// tracker.go 实现项目跟踪状态 (Sprint 22).
//
// 数据模型: TrackingState (含 characters/foreshadowing/timeline/graph/recent_summaries)
// 持久化: data/_tracking-state.json (单文件 schema_version=2)
//
// 参考 Python V0.22 core/memory/tracker.py.
import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// CharacterState 角色状态 (tracker 子结构).
type CharacterState struct {
	Name               string   `json:"name"`
	Location           string   `json:"location,omitempty"`
	EmotionalState     string   `json:"emotional_state,omitempty"`
	Motivation         string   `json:"motivation,omitempty"`
	Knowledge          []string `json:"knowledge"`
	LastUpdatedChapter int      `json:"last_updated_chapter"`
}

// ForeshadowingState 伏笔状态.
type ForeshadowingState struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	PlantedChapter int    `json:"planted_chapter"`
	Status         string `json:"status"` // active / advanced / revealed
	Notes          string `json:"notes,omitempty"`
}

// TimelineEvent 时间线事件.
type TimelineEvent struct {
	Chapter      int      `json:"chapter"`
	InWorldTime  string   `json:"in_world_time"` // 例如 "三年夏"
	Event        string   `json:"event"`
	RelatedChars []string `json:"related_chars,omitempty"`
}

// TrackingState 项目跟踪状态 (单文件 schema).
type TrackingState struct {
	SchemaVersion        int    `json:"schema_version"` // 默认 2
	ProjectName          string `json:"project_name"`
	Genre                string `json:"genre,omitempty"`
	StyleAnchor          string `json:"style_anchor,omitempty"`
	TotalChaptersTarget  int    `json:"total_chapters_target,omitempty"`
	TotalWordCountTarget int    `json:"total_word_count_target,omitempty"`

	Characters    map[string]CharacterState     `json:"characters"`
	Foreshadowing map[string]ForeshadowingState `json:"foreshadowing"`
	Timeline      []TimelineEvent               `json:"timeline"`

	// L5 知识图谱 (V0.22+)
	Graph *GraphData `json:"graph"`

	// 最近章节摘要 (L3 滑动窗口)
	RecentChapterSummaries map[int]string `json:"recent_chapter_summaries"`

	LastUpdatedChapter int        `json:"last_updated_chapter"`
	LastUpdatedAt      *time.Time `json:"last_updated_at,omitempty"`
}

// newEmptyState 创建空 state (项目初始化用).
func newEmptyState(projectName string) *TrackingState {
	graph := &GraphData{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	return &TrackingState{
		SchemaVersion:          2,
		ProjectName:            projectName,
		Characters:             make(map[string]CharacterState),
		Foreshadowing:          make(map[string]ForeshadowingState),
		Timeline:               []TimelineEvent{},
		Graph:                  graph,
		RecentChapterSummaries: make(map[int]string),
	}
}

// Tracker 状态管理 + JSON 持久化.
type Tracker struct {
	stateFile string
	mu        sync.RWMutex
}

// NewTracker 创建 tracker (state_file 一般为 data/_tracking-state.json).
func NewTracker(stateFile string) *Tracker {
	return &Tracker{stateFile: stateFile}
}

// Exists 检查 state 文件是否存在.
func (t *Tracker) Exists() bool {
	_, err := os.Stat(t.stateFile)
	return err == nil
}

// Read 读取 state (文件不存在返回空 state, 不报错).
func (t *Tracker) Read() (*TrackingState, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	data, err := os.ReadFile(t.stateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 返回 empty state (caller 决定是否 init)
			return newEmptyState(""), nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}

	var s TrackingState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &s, nil
}

// Write 原子写入 state (写到 .tmp 再 rename).
func (t *Tracker) Write(state *TrackingState) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	state.LastUpdatedAt = ptrTime(time.Now().UTC())
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// 确保父目录存在
	if dir := fileDir(t.stateFile); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir parent: %w", err)
		}
	}

	// 写到 tmp 文件
	tmp := t.stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	// atomic rename
	if err := os.Rename(tmp, t.stateFile); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// fileDir 返回 path 的父目录 (helper, 避免 import path/filepath).
func fileDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return ""
}

// Init 初始化空 state (项目不存在 state 文件时调用).
func (t *Tracker) Init(projectName string) (*TrackingState, error) {
	state := newEmptyState(projectName)
	if err := t.Write(state); err != nil {
		return nil, err
	}
	return state, nil
}

// Merge 合并两个 state (新 chapter 写入时 merge into 当前 state).
//
// 当前实现: 全量覆盖 LastUpdatedChapter + 添加新 characters/foreshadowing/timeline/summaries.
// 注: 不做 deep merge (e.g. character fields merge), Sprint 23 Extractor.ApplyToState 处理.
func (t *Tracker) Merge(current, extracted *TrackingState) *TrackingState {
	// TODO Sprint 24 用 Extractor.ApplyToState 实现精细 merge
	// 当前简化为: 直接覆盖 extracted 字段
	current.LastUpdatedChapter = extracted.LastUpdatedChapter
	current.Characters = extracted.Characters
	current.Foreshadowing = extracted.Foreshadowing
	current.Timeline = extracted.Timeline
	if extracted.Graph != nil {
		current.Graph = extracted.Graph
	}
	for ch, summary := range extracted.RecentChapterSummaries {
		current.RecentChapterSummaries[ch] = summary
	}
	return current
}

// GetCharacter 取角色状态.
func (t *Tracker) GetCharacter(state *TrackingState, name string) (*CharacterState, bool) {
	c, ok := state.Characters[name]
	if !ok {
		return nil, false
	}
	return &c, true
}

// GetActiveForeshadowing 取所有 active 状态的伏笔.
func (t *Tracker) GetActiveForeshadowing(state *TrackingState) []ForeshadowingState {
	out := make([]ForeshadowingState, 0)
	for _, fs := range state.Foreshadowing {
		if fs.Status == "active" {
			out = append(out, fs)
		}
	}
	return out
}

// GetRecentSummaries 取最近 N 章摘要 (按 chapter DESC).
func (t *Tracker) GetRecentSummaries(state *TrackingState, n int) []ChapterSummary {
	out := make([]ChapterSummary, 0, n)
	for ch, summary := range state.RecentChapterSummaries {
		out = append(out, ChapterSummary{Chapter: ch, Summary: summary})
	}
	// 排序 (按 chapter DESC)
	sortByChapterDesc(out)
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// ChapterSummary chapter + summary 对.
type ChapterSummary struct {
	Chapter int    `json:"chapter"`
	Summary string `json:"summary"`
}

// sortByChapterDesc 按 chapter 降序 (simple bubble sort, n 通常 < 20).
func sortByChapterDesc(s []ChapterSummary) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j].Chapter > s[i].Chapter {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// ptrTime 返回 time.Time 指针 (helper).
func ptrTime(t time.Time) *time.Time {
	return &t
}
