// Package llm 提供 LLM 多 provider 路由 + 流式响应.
package llm

// adaptive_router.go 实现自适应路由 (Sprint 21 补全).
//
// 设计:
//   - 每次 LLM 调用后 Record(task, model, success)
//   - Select(task) 根据历史数据选最佳 model
//   - 4 种策略: best_avg / best_quality / best_speed / best_success
//   - 冷启动: 样本数 < minSamples 时返回空 (caller fallback 默认路由)
//
// 数据存储: store.RoutingStore (SQLite adaptive_routing 表, schema003)
//
// 注: llm 包不直接 import store (保持依赖单向 store→llm 通过 cache.go 反过来,
//     但 store 没有 import llm 所以无循环). 通过 interface 注入 store.
//
// 参考 Python V0.30.6 C3 core/adaptive_router.py.
import (
	"context"
	"errors"
	"sort"
)

// AdaptiveStrategy 路由策略.
type AdaptiveStrategy string

const (
	StrategyBestAvg     AdaptiveStrategy = "best_avg"     // 加权综合分 (推荐)
	StrategyBestQuality AdaptiveStrategy = "best_quality" // 优先质量
	StrategyBestSpeed   AdaptiveStrategy = "best_speed"   // 优先速度
	StrategyBestSuccess AdaptiveStrategy = "best_success" // 优先成功率
)

// ModelStats 单模型在单任务上的统计数据.
type ModelStats struct {
	Model        string   `json:"model"`
	Task         TaskType `json:"task"`
	Samples      int64    `json:"samples"`
	SuccessCount int64    `json:"success_count"`
	SuccessRate  float64  `json:"success_rate"`
	AvgLatencyMS float64  `json:"avg_latency_ms"` // 来自 metrics 包 (V0 暂未填充)
	AvgQuality   float64  `json:"avg_quality"`    // V0 占位
	Score        float64  `json:"score"`
}

// RoutingDataSource interface for AdaptiveRouter 注入.
//
// store.RoutingStore 实现此 interface (RecordResult + ListByTask).
type RoutingDataSource interface {
	RecordResult(ctx context.Context, task, model string, success bool) error
	ListByTask(ctx context.Context, task string) ([]ModelStats, error)
}

// AdaptiveRouter 自适应路由器.
type AdaptiveRouter struct {
	source     RoutingDataSource
	strategy   AdaptiveStrategy
	minSamples int64
}

// NewAdaptiveRouter 创建.
//
// strategy 为空用 best_avg.
// minSamples 为冷启动阈值 (推荐 5).
func NewAdaptiveRouter(source RoutingDataSource, strategy AdaptiveStrategy, minSamples int64) *AdaptiveRouter {
	if strategy == "" {
		strategy = StrategyBestAvg
	}
	if minSamples <= 0 {
		minSamples = 5
	}
	return &AdaptiveRouter{source: source, strategy: strategy, minSamples: minSamples}
}

// Record 记录一次 LLM 调用结果.
func (r *AdaptiveRouter) Record(ctx context.Context, task TaskType, model string, success bool, latencyMS float64, quality float64) error {
	if r.source == nil {
		return ErrNoStore
	}
	if err := r.source.RecordResult(ctx, string(task), model, success); err != nil {
		return err
	}
	// 注: V0 只用 success_rate. latency/quality 留给 metrics 包.
	_ = latencyMS
	_ = quality
	return nil
}

// Select 根据历史数据选最佳 model.
//
// 返回:
//   - model name: 用此 model
//   - "": 样本不足, caller 应用默认路由 (router.resolve)
func (r *AdaptiveRouter) Select(ctx context.Context, task TaskType) (string, error) {
	if r.source == nil {
		return "", ErrNoStore
	}
	stats, err := r.source.ListByTask(ctx, string(task))
	if err != nil {
		return "", err
	}

	// 过滤样本数
	eligible := make([]ModelStats, 0, len(stats))
	for _, s := range stats {
		if s.Samples >= r.minSamples {
			eligible = append(eligible, s)
		}
	}
	if len(eligible) == 0 {
		return "", nil // 冷启动
	}

	// 计算综合分
	for i := range eligible {
		eligible[i].Score = r.computeScore(eligible[i])
	}

	// 按 score DESC 排序
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].Score > eligible[j].Score
	})

	return eligible[0].Model, nil
}

// computeScore 按 strategy 计算综合分.
func (r *AdaptiveRouter) computeScore(s ModelStats) float64 {
	switch r.strategy {
	case StrategyBestQuality:
		return s.AvgQuality
	case StrategyBestSpeed:
		if s.AvgLatencyMS > 0 {
			return 1000.0 / s.AvgLatencyMS
		}
		return 0
	case StrategyBestSuccess:
		return s.SuccessRate
	case StrategyBestAvg, "":
		fallthrough
	default:
		// best_avg: success_rate * 100 (简化, V0 占位)
		return s.SuccessRate * 100
	}
}

// ErrNoStore data source 为 nil.
var ErrNoStore = errors.New("adaptive_router: data source is nil")
