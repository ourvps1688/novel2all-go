package api

import (
	"encoding/json"
	"net/http"
	"runtime"
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

// CacheHandler GET /api/cache/stats + /api/cache/prompt-stats
//
// P1-F 阶段：先 mock（真实 cache 实现需要 LLM provider 集成，P2 阶段）
// 用原子计数器 + runtime 统计代替真实 LLM cache
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

// ServeHTTP 路由
func (h *CacheHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/cache/stats":
		h.handleStats(w, r)
	case "/api/cache/prompt-stats":
		h.handlePromptStats(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *CacheHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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

func (h *CacheHandler) handlePromptStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
		CostSavedCNY:        0.0, // mock
		PotentialSavingsCNY: 0.0, // mock
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(stats)
}

// runtime 统计（mock endpoint 可用）
var _ = runtime.NumGoroutine
