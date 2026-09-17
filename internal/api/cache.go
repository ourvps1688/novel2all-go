package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// CacheStats LLM cache 运行时统计
type CacheStats struct {
	HitRate     float64 `json:"hit_rate"`
	Hits        int64   `json:"hits"`
	Misses      int64   `json:"misses"`
	Size        int     `json:"size"`
	MaxSize     int     `json:"max_size"`
	Backend     string  `json:"backend"`
	LockBackend string  `json:"lock_backend"`
	TTLSeconds  int     `json:"ttl_seconds"`
}

// PromptCacheStats Prompt prefix cache 统计
type PromptCacheStats struct {
	PrefixHits          int64   `json:"prefix_hits"`
	PrefixMisses        int64   `json:"prefix_misses"`
	Total               int64   `json:"total"`
	HitRate             float64 `json:"hit_rate"`
	CostSavedCNY        float64 `json:"cost_saved_cny"`
	PotentialSavingsCNY float64 `json:"potential_savings_cny"`
}

// MigrateResult cache 迁移结果
type MigrateResult struct {
	Src           string `json:"src"`
	Dst           string `json:"dst"`
	SrcBackend    string `json:"src_backend"`
	DstBackend    string `json:"dst_backend"`
	Migrated      int    `json:"migrated"`
	Skipped       int    `json:"skipped"`
	Errors        int    `json:"errors"`
	ElapsedMS     int64  `json:"elapsed_ms"`
	DstMaxSize    int    `json:"dst_max_size"`
	DstTTLSeconds int    `json:"dst_ttl_seconds"`
	Message       string `json:"message,omitempty"`
}

// RecommendResult cache 推荐配置结果
type RecommendResult struct {
	Current     map[string]any    `json:"current"`
	Recommended map[string]any    `json:"recommended"`
	Actions     []RecommendAction `json:"actions"`
	HealthScore float64           `json:"health_score"`
	Issues      []string          `json:"issues"`
	Confidence  string            `json:"confidence"`
	Notes       []string          `json:"notes"`
}

// RecommendAction 单个推荐操作
type RecommendAction struct {
	Action      string `json:"action"`
	Description string `json:"description"`
	Impact      string `json:"impact"`
	HowTo       string `json:"how_to"`
}

// CacheHandler GET /api/cache/stats + /api/cache/prompt-stats
// + POST /api/cache/migrate + GET /api/cache/recommend + POST /api/cache/reset
type CacheHandler struct {
	startTime time.Time
}

// NewCacheHandler 创建
func NewCacheHandler() *CacheHandler {
	return &CacheHandler{startTime: time.Now()}
}

// 简单统计（mock）
var (
	cacheHits    int64
	cacheMisses  int64
	prefixHits   int64
	prefixMisses int64
	cacheSize    int64
	cacheMaxSize int64 = 1000
	cacheTTLSec  int64 = 3600
)

// ServeHTTP 路由分发
func (h *CacheHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/cache/")
	path = strings.Trim(path, "/")
	switch path {
	case "stats":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleStats(w, r)
	case "prompt-stats":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handlePromptStats(w, r)
	case "migrate":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleMigrate(w, r)
	case "recommend":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleRecommend(w, r)
	case "reset":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleReset(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *CacheHandler) handleStats(w http.ResponseWriter, _ *http.Request) {
	hits := atomic.LoadInt64(&cacheHits)
	misses := atomic.LoadInt64(&cacheMisses)
	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}
	stats := CacheStats{
		HitRate:     hitRate,
		Hits:        hits,
		Misses:      misses,
		Size:        int(atomic.LoadInt64(&cacheSize)),
		MaxSize:     int(atomic.LoadInt64(&cacheMaxSize)),
		Backend:     "memory",
		LockBackend: "sync.RWMutex",
		TTLSeconds:  int(atomic.LoadInt64(&cacheTTLSec)),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *CacheHandler) handlePromptStats(w http.ResponseWriter, _ *http.Request) {
	hits := atomic.LoadInt64(&prefixHits)
	misses := atomic.LoadInt64(&prefixMisses)
	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}
	stats := PromptCacheStats{
		PrefixHits:          hits,
		PrefixMisses:        misses,
		Total:               total,
		HitRate:             hitRate,
		CostSavedCNY:        0.0,
		PotentialSavingsCNY: 0.0,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(stats)
}

// handleMigrate 在 cache backend 之间迁移（P1-F 切片 7）
//
// Form params:
//   - src       (required) 源 cache 文件路径
//   - dst       (required) 目标 cache 文件路径
//   - max_size  (optional) 目标 max_size
//   - ttl_seconds (optional) 目标 TTL
//
// 简化实现：JSON file → JSON file（不支持 sqlite/redis 后端）
func (h *CacheHandler) handleMigrate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid form"}`, http.StatusBadRequest)
		return
	}
	src := r.FormValue("src")
	dst := r.FormValue("dst")
	if src == "" || dst == "" {
		http.Error(w, `{"error":"src and dst required"}`, http.StatusBadRequest)
		return
	}
	maxSize := 1000
	if v := r.FormValue("max_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxSize = n
		}
	}
	ttl := 0
	if v := r.FormValue("ttl_seconds"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ttl = n
		}
	}

	start := time.Now()
	result := MigrateResult{
		Src:           src,
		Dst:           dst,
		SrcBackend:    detectBackend(src),
		DstBackend:    detectBackend(dst),
		DstMaxSize:    maxSize,
		DstTTLSeconds: ttl,
	}

	// 读源
	srcEntries, err := readCacheJSON(src)
	if err != nil {
		result.Errors = 1
		result.Message = fmt.Sprintf("read src: %v", err)
		writeCacheResult(w, http.StatusBadRequest, result)
		return
	}

	// 写到目标
	if err := writeCacheJSON(dst, srcEntries, maxSize, ttl); err != nil {
		result.Errors = 1
		result.Message = fmt.Sprintf("write dst: %v", err)
		writeCacheResult(w, http.StatusInternalServerError, result)
		return
	}

	result.Migrated = len(srcEntries)
	result.ElapsedMS = time.Since(start).Milliseconds()
	result.Message = fmt.Sprintf("migrated %d entries from %s to %s", len(srcEntries), result.SrcBackend, result.DstBackend)
	writeCacheResult(w, http.StatusOK, result)
}

// handleRecommend 推荐 cache 配置（基于当前 stats）
func (h *CacheHandler) handleRecommend(w http.ResponseWriter, _ *http.Request) {
	hits := atomic.LoadInt64(&cacheHits)
	misses := atomic.LoadInt64(&cacheMisses)
	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}
	size := int(atomic.LoadInt64(&cacheSize))
	maxSize := int(atomic.LoadInt64(&cacheMaxSize))
	ttl := int(atomic.LoadInt64(&cacheTTLSec))

	issues := []string{}
	actions := []RecommendAction{}
	notes := []string{}
	healthScore := 1.0

	if total == 0 {
		notes = append(notes, "数据不足（无 hit/miss 记录），置信度低")
		healthScore = 0.5
	} else if hitRate < 0.3 {
		issues = append(issues, fmt.Sprintf("hit rate 偏低 (%.1f%%)", hitRate*100))
		actions = append(actions, RecommendAction{
			Action:      "increase_max_size",
			Description: "增大 max_size 可减少 eviction，提升 hit rate",
			Impact:      "高",
			HowTo:       "POST /api/cache/migrate?max_size=5000 (增大到 5000)",
		})
		healthScore -= 0.3
	}

	if size >= maxSize*9/10 {
		issues = append(issues, fmt.Sprintf("cache 接近满载 (%d/%d)", size, maxSize))
		actions = append(actions, RecommendAction{
			Action:      "increase_max_size",
			Description: "cache 满了会触发 LRU eviction",
			Impact:      "高",
			HowTo:       "POST /api/cache/migrate?max_size=2000",
		})
		healthScore -= 0.2
	}

	if ttl > 0 && ttl < 600 {
		issues = append(issues, "TTL 过短（< 10 分钟）")
		actions = append(actions, RecommendAction{
			Action:      "increase_ttl",
			Description: "短 TTL 会频繁 expire",
			Impact:      "中",
			HowTo:       "POST /api/cache/migrate?ttl_seconds=3600",
		})
		healthScore -= 0.1
	}

	if len(actions) == 0 && len(issues) == 0 {
		actions = append(actions, RecommendAction{
			Action:      "no_action",
			Description: "当前 cache 配置良好，无需调整",
			Impact:      "无",
			HowTo:       "持续观察 stats",
		})
	}

	confidence := "high"
	if total < 100 {
		confidence = "low"
	} else if total < 1000 {
		confidence = "medium"
	}
	if healthScore < 0 {
		healthScore = 0
	}

	result := RecommendResult{
		Current: map[string]any{
			"backend":     "memory",
			"max_size":    maxSize,
			"ttl_seconds": ttl,
			"size":        size,
			"hit_rate":    hitRate,
		},
		Recommended: map[string]any{
			"backend":     "memory",
			"max_size":    maxSize,
			"ttl_seconds": ttl,
		},
		Actions:     actions,
		HealthScore: healthScore,
		Issues:      issues,
		Confidence:  confidence,
		Notes:       notes,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}

// handleReset 重置 cache（清空计数器 + 删除持久化文件）
func (h *CacheHandler) handleReset(w http.ResponseWriter, _ *http.Request) {
	// 重置所有计数器
	atomic.StoreInt64(&cacheHits, 0)
	atomic.StoreInt64(&cacheMisses, 0)
	atomic.StoreInt64(&prefixHits, 0)
	atomic.StoreInt64(&prefixMisses, 0)
	atomic.StoreInt64(&cacheSize, 0)

	// 删除 data/cache.json（如果存在）
	cachePath := "data/cache.json"
	if _, err := os.Stat(cachePath); err == nil {
		_ = os.Remove(cachePath)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"reset":               true,
		"hits_after":          0,
		"misses_after":        0,
		"prefix_hits_after":   0,
		"prefix_misses_after": 0,
		"size_after":          0,
		"message":             "all cache counters reset; persistent file removed if existed",
	})
}

// detectBackend 根据扩展名检测 backend
func detectBackend(path string) string {
	if strings.HasSuffix(path, ".json") {
		return "json"
	}
	if strings.HasSuffix(path, ".db") || strings.HasSuffix(path, ".sqlite") {
		return "sqlite"
	}
	if strings.HasPrefix(path, "redis://") {
		return "redis"
	}
	return "unknown"
}

// cacheEntry JSON cache entry
type cacheEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

// readCacheJSON 读 cache JSON 文件（兼容 wrapper 和裸 entries 数组两种格式）
func readCacheJSON(path string) ([]cacheEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	// 先尝试 wrapper 格式 {"entries": [...], "max_size": ...}
	var wrapper struct {
		Entries []cacheEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Entries != nil {
		return wrapper.Entries, nil
	}
	// fallback: 裸 entries 数组
	var entries []cacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// writeCacheJSON 写 cache JSON 文件
func writeCacheJSON(path string, entries []cacheEntry, maxSize int, ttlSeconds int) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	wrapper := map[string]any{
		"max_size":    maxSize,
		"ttl_seconds": ttlSeconds,
		"entries":     entries,
		"migrated_at": time.Now().Unix(),
	}
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// writeCacheResult 写迁移结果响应
func writeCacheResult(w http.ResponseWriter, status int, result MigrateResult) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

// 防止 unused 警告
var _ = io.Discard
var _ = runtime.NumGoroutine
