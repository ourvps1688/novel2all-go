// Package memory 提供 novel2all-go 长记忆系统 (Sprint 22+).
//
// Sprint 34: MemoryGraphTools 把 MemoryGraph / Tracker methods 暴露为
// OpenAI / Anthropic tool call, 让 LLM 在写作时可主动查询.
package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// ToolsOpts MemoryTools Graph + Tracker 暴露的 tool 集合 (Sprint 34).
//
// 用法:
//
//	tools := memory.MemoryTools(memMgr)
//	resp, err := router.ChatWithTools(ctx, llm.ChatWithToolsRequest{
//	    Messages: ...,
//	    Tools:    tools,
//	})
type ToolsOpts struct {
	// MemMgr 必填 (提供 Graph + Tracker access)
	MemMgr *MemoryManager
}

// MemoryTools 返回 4 个 tool 定义: graph_query / foreshadow_query /
// timeline_query / character_query. 全部 duck-type 实现 llm.Tool.
//
// Sprint 34 简化: 返回 []llm.Tool (而不是单一 struct), 让调用方自己拼接.
//
// 参数 schema 用 JSON Schema (OpenAI / Anthropic 都用), handler 内部
// 调 MemoryManager 的 Graph/Tracker methods.
func Tools(mm *MemoryManager) []llm.Tool {
	return []llm.Tool{
		graphQueryTool(mm),
		foreshadowQueryTool(mm),
		timelineQueryTool(mm),
		characterQueryTool(mm),
	}
}

// graphQueryTool 角色关系图查询 (from → to 路径, 或指定 type 关系).
func graphQueryTool(mm *MemoryManager) llm.Tool {
	return llm.Tool{
		Name: "graph_query",
		Description: "查询知识图谱中两个角色之间的关系路径。" +
			"返回最短路径上的节点 + 关系 type。" +
			"例: 查林雷→霍格的关系链 (师徒? 朋友?)",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"from": {"type": "string", "description": "起点角色名"},
				"to":   {"type": "string", "description": "终点角色名"},
				"type": {"type": "string", "description": "可选: 关系类型过滤 (e.g. 'friend'/'enemy'/'family')"}
			},
			"required": ["from", "to"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var p struct {
				From string `json:"from"`
				To   string `json:"to"`
				Type string `json:"type"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("graph_query: bad args: %w", err)
			}
			g := mm.Graph()
			if g == nil {
				return nil, fmt.Errorf("graph not initialized")
			}
			// 简化: BFSPaths 返回 from→to 路径
			paths := g.BFSPaths(p.From, p.To, 5) // max depth 5
			return map[string]any{
				"from":  p.From,
				"to":    p.To,
				"paths": paths,
				"count": len(paths),
			}, nil
		},
	}
}

// foreshadowQueryTool 查询活跃伏笔 (按章节范围).
func foreshadowQueryTool(mm *MemoryManager) llm.Tool {
	return llm.Tool{
		Name: "foreshadow_query",
		Description: "查询当前还活跃的伏笔 (未回收)。" +
			"返回伏笔列表: 内容 + 埋设章节 + 状态.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"chapter": {"type": "integer", "description": "可选: 限定章节范围 (查 N 章前埋的)"}
			}
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var p struct {
				Chapter int `json:"chapter"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("foreshadow_query: bad args: %w", err)
			}
			state, err := mm.LoadState(mm.projectName())
			if err != nil {
				return nil, err
			}
			active := mm.Tracker().GetActiveForeshadowing(state)
			// 可选: 限定到 chapter 之前
			if p.Chapter > 0 {
				filtered := make([]ForeshadowingState, 0, len(active))
				for _, f := range active {
					if f.PlantedChapter <= p.Chapter {
						filtered = append(filtered, f)
					}
				}
				active = filtered
			}
			return map[string]any{
				"count":       len(active),
				"foreshadows": active,
			}, nil
		},
	}
}

// timelineQueryTool 查询时间线事件 (章节范围).
func timelineQueryTool(mm *MemoryManager) llm.Tool {
	return llm.Tool{
		Name: "timeline_query",
		Description: "查询章节时间线事件 (从 recent_chapter_summaries 取). " +
			"返回 [from, to] 章节范围内的所有摘要.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"from": {"type": "integer"},
				"to":   {"type": "integer"}
			},
			"required": ["from", "to"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var p struct {
				From int `json:"from"`
				To   int `json:"to"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("timeline_query: bad args: %w", err)
			}
			state, err := mm.LoadState(mm.projectName())
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, 0)
			for ch := p.From; ch <= p.To; ch++ {
				if s, ok := state.RecentChapterSummaries[ch]; ok {
					out = append(out, map[string]any{
						"chapter": ch,
						"summary": s,
					})
				}
			}
			return map[string]any{
				"from":    p.From,
				"to":      p.To,
				"entries": out,
				"count":   len(out),
			}, nil
		},
	}
}

// characterQueryTool 查询角色状态.
func characterQueryTool(mm *MemoryManager) llm.Tool {
	return llm.Tool{
		Name: "character_query",
		Description: "查询角色当前状态 (位置/情绪/动机/已知信息). " +
			"返回该角色在 tracking_state 里的完整快照.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "角色名"}
			},
			"required": ["name"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var p struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("character_query: bad args: %w", err)
			}
			state, err := mm.LoadState(mm.projectName())
			if err != nil {
				return nil, err
			}
			cs, ok := state.Characters[p.Name]
			if !ok {
				return map[string]any{
					"found": false,
					"name":  p.Name,
					"hint":  "角色不在 tracking_state.Characters. 用 character_query 之前先写角色设定.",
				}, nil
			}
			return map[string]any{
				"found":                true,
				"name":                 p.Name,
				"location":             cs.Location,
				"emotional":            cs.EmotionalState,
				"motivation":           cs.Motivation,
				"knowledge":            cs.Knowledge,
				"last_updated_chapter": cs.LastUpdatedChapter,
			}, nil
		},
	}
}
