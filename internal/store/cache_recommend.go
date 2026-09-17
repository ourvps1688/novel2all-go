// Package store 提供 SQLite 存储 + 表 CRUD.
package store

// cache_recommend.go 实现 cache 配置智能推荐 (Sprint 21 补全).
//
// 设计目标:
//   - 根据当前 cache stats 给出 backend / max_size / ttl_seconds 建议
//   - 数据驱动 (基于 V0.47 benchmark + V0.49 调优指南规则)
//   - 输出可立即执行的 action 列表 (含理由 + 预期收益)
//
// 输出结构:
//
//	{
//	  "current": {"backend": "sqlite", "max_size": 1024, "ttl_seconds": 0},
//	  "recommended": {
//	    "backend": {"current": "sqlite", "recommended": "sqlite", "reason": "...", ...},
//	    "max_size": {...},
//	    "ttl_seconds": {...}
//	  },
//	  "actions": [{"priority": "high", "action": "...", "reason": "..."}],
//	  "health_score": 0.85,
//	  "issues": ["size_full"],
//	  "confidence": "high",
//	  "notes": []
//	}
//
// 参考 Python V0.51 core/cache_recommend.py.
import "math"

// CacheRecommendationStats cache 当前统计 (input).
type CacheRecommendationStats struct {
	Backend    string  // "memory" | "sqlite" | "redis" | "json"
	MaxSize    int     // L1 memory cache cap
	TTLSeconds int     // 0 = no expiry
	Hits       int64   `json:"hits"`
	Misses     int64   `json:"misses"`
	Size       int     // 当前 entry 数
	HitRate    float64 // 0.0 - 1.0
}

// FieldRecommendation 单字段推荐.
type FieldRecommendation struct {
	Current     any    `json:"current"`
	Recommended any    `json:"recommended"`
	Reason      string `json:"reason"`
	Impact      string `json:"impact,omitempty"`
	Confidence  string `json:"confidence"` // low | medium | high
}

// CacheRecommendation 完整 cache 配置推荐.
type CacheRecommendation struct {
	Current     map[string]any                 `json:"current"`
	Recommended map[string]FieldRecommendation `json:"recommended"`
	Actions     []map[string]any               `json:"actions"`
	HealthScore float64                        `json:"health_score"`
	Issues      []string                       `json:"issues"`
	Confidence  string                         `json:"confidence"`
	Notes       []string                       `json:"notes"`
}

// Recommend 根据 cache stats 生成配置推荐.
//
// V0 简化版: 只检查 3 个字段 (backend / max_size / ttl_seconds),
// 不依赖 stats 完整数据.
//
//nolint:gocyclo // cache 推荐的多种 if 分支 (backend × size × ttl × health 各自独立逻辑)
func Recommend(stats CacheRecommendationStats) CacheRecommendation {
	current := map[string]any{
		"backend":     stats.Backend,
		"max_size":    stats.MaxSize,
		"ttl_seconds": stats.TTLSeconds,
	}

	recommended := make(map[string]FieldRecommendation)
	var actions []map[string]any
	var issues []string

	// === 1. Backend 推荐 (switch on backend type)
	switch stats.Backend {
	case "memory", "json":
		recommended["backend"] = FieldRecommendation{
			Current:     stats.Backend,
			Recommended: "sqlite",
			Reason:      "memory/json 写性能低 (json ~291 ops/s), sqlite ~54K ops/s",
			Impact:      "性能提升 45-185x",
			Confidence:  "high",
		}
		actions = append(actions, map[string]any{
			"priority": "high",
			"action":   "switch_backend",
			"from":     stats.Backend,
			"to":       "sqlite",
			"reason":   "sqlite 写性能 185x, 读 45x",
		})
		issues = append(issues, "backend_suboptimal")
	default:
		recommended["backend"] = FieldRecommendation{
			Current:     stats.Backend,
			Recommended: stats.Backend,
			Reason:      "已是最优 backend",
			Confidence:  "high",
		}
	}

	// === 2. max_size 推荐 (按 cache state 分类)
	recommended["max_size"] = recommendMaxSize(stats, actions, &issues)

	// === 3. ttl_seconds 推荐 ===
	if stats.TTLSeconds == 0 {
		recommended["ttl_seconds"] = FieldRecommendation{
			Current:     0,
			Recommended: 86400, // 24h
			Reason:      "永久 cache 可能保留过期响应, 推荐 24h TTL",
			Impact:      "减少 stale response",
			Confidence:  "low",
		}
		actions = append(actions, map[string]any{
			"priority": "low",
			"action":   "set_ttl",
			"to":       86400,
			"reason":   "24h TTL 平衡新鲜度与性能",
		})
		issues = append(issues, "no_ttl")
	} else {
		recommended["ttl_seconds"] = FieldRecommendation{
			Current:     stats.TTLSeconds,
			Recommended: stats.TTLSeconds,
			Reason:      "TTL 已设置",
			Confidence:  "high",
		}
	}

	// === Health Score ===
	healthScore := 1.0
	// no data → health 0.5 (中等, 不健康)
	if stats.Hits+stats.Misses == 0 {
		healthScore = 0.5
	}
	if stats.HitRate > 0 && stats.HitRate < 0.6 {
		healthScore -= 0.3
	}
	if stats.Size >= stats.MaxSize && stats.MaxSize > 0 {
		healthScore -= 0.2
	}
	if stats.TTLSeconds == 0 {
		healthScore -= 0.1
	}
	healthScore = math.Max(0, healthScore)

	// === Confidence ===
	confidence := "high"
	if stats.Hits+stats.Misses < 100 {
		confidence = "medium"
	}
	if stats.Hits+stats.Misses < 10 {
		confidence = "low"
	}

	return CacheRecommendation{
		Current:     current,
		Recommended: recommended,
		Actions:     actions,
		HealthScore: healthScore,
		Issues:      issues,
		Confidence:  confidence,
		Notes:       []string{},
	}
}

// fmtFloat 简化版 fmt.Sprintf("%.1f", x).
func fmtFloat(x float64, prec int) string {
	if prec == 1 {
		// 1 decimal
		intPart := int(x)
		frac := int((x - float64(intPart)) * 10)
		if frac < 0 {
			frac = -frac
		}
		return itoa(intPart) + "." + itoa(frac)
	}
	return itoa(int(x))
}

// itoa 简化版 strconv.Itoa.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// recommendMaxSize 根据 cache state 推荐 max_size 配置.
//
// 返回 FieldRecommendation 同时 append actions + issues.
func recommendMaxSize(stats CacheRecommendationStats, actions []map[string]any, issues *[]string) FieldRecommendation {
	switch {
	case stats.HitRate > 0 && stats.HitRate < 0.6:
		newSize := stats.MaxSize * 2
		if stats.MaxSize == 0 {
			newSize = 1024
		}
		*issues = append(*issues, "size_too_small")
		return FieldRecommendation{
			Current:     stats.MaxSize,
			Recommended: newSize,
			Reason:      fmtFloat(stats.HitRate*100, 1) + "% hit rate 偏低, cache 满了",
			Impact:      "预期 hit rate 提升到 70%+",
			Confidence:  "medium",
		}
	case stats.Size > 0 && stats.Size >= stats.MaxSize:
		newSize := stats.MaxSize * 2
		*issues = append(*issues, "size_full")
		return FieldRecommendation{
			Current:     stats.MaxSize,
			Recommended: newSize,
			Reason:      "cache 已满 (" + itoa(stats.Size) + "/" + itoa(stats.MaxSize) + ")",
			Impact:      "避免 evict 热点",
			Confidence:  "high",
		}
	default:
		return FieldRecommendation{
			Current:     stats.MaxSize,
			Recommended: stats.MaxSize,
			Reason:      "size 健康",
			Confidence:  "high",
		}
	}
}
