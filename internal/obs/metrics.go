// Package obs 提供可观测性原语：结构化日志 + metrics + traces。
//
// P0 阶段只实现 slog 封装（logger.go）。
// P1-F 切片 9 加入 metrics 和 traces。
package obs

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 进程级 metrics 注册表（线程安全）。
//
// 设计原则：
//   - 不引入 prometheus client_golang 依赖（避免 go.mod toolchain 升级）
//   - 手写最小 Prometheus text format 输出（version 0.0.4）
//   - atomic.Int64 用于计数器；RWMutex 保护 map 创建
//   - Snapshot() 返回不可变快照，避免输出期间 map 被改
//
// 标签基数控制：所有 counter 都以 method/path/status 或 provider/task/status 为 key，
// 路径不做 ID 归一化（P1-F 切片 9 简化版；后续如需要可加 trie）。
type Metrics struct {
	mu sync.RWMutex

	// HTTP 请求计数：key = "METHOD|PATH|STATUS"
	httpRequests map[string]*atomic.Int64

	// HTTP 请求耗时：key = "METHOD|PATH"，分别存 sum_ms 和 count
	httpDurationSum   map[string]*atomic.Int64
	httpDurationCount map[string]*atomic.Int64

	// LLM 调用计数：key = "PROVIDER|TASK|STATUS"
	llmCalls map[string]*atomic.Int64

	// LLM tokens：key = "PROVIDER|KIND"（kind = prompt | completion）
	llmTokens map[string]*atomic.Int64

	// 单值指标
	sseActiveStreams atomic.Int64
	cacheHitsTotal   atomic.Int64
	cacheMissesTotal atomic.Int64
	dbSessionsActive atomic.Int64

	startTime time.Time

	// 静态构建信息（注入）
	version   string
	commit    string
	goVersion string
}

// NewMetrics 创建并初始化 Metrics 注册表。
//
// version/commit/goVersion 注入到 build_info 指标。
func NewMetrics(version, commit, goVersion string) *Metrics {
	return &Metrics{
		httpRequests:      make(map[string]*atomic.Int64),
		httpDurationSum:   make(map[string]*atomic.Int64),
		httpDurationCount: make(map[string]*atomic.Int64),
		llmCalls:          make(map[string]*atomic.Int64),
		llmTokens:         make(map[string]*atomic.Int64),
		startTime:         time.Now(),
		version:           version,
		commit:            commit,
		goVersion:         goVersion,
	}
}

// getOrCreate 获取或创建计数器（双检锁）。
func (m *Metrics) getOrCreate(mp map[string]*atomic.Int64, key string) *atomic.Int64 {
	m.mu.RLock()
	c, ok := mp[key]
	m.mu.RUnlock()
	if ok {
		return c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := mp[key]; ok {
		return c
	}
	c = &atomic.Int64{}
	mp[key] = c
	return c
}

// IncHTTPRequests 增加 HTTP 请求计数。
func (m *Metrics) IncHTTPRequests(method, path string, status int) {
	key := httpKey(method, path, status)
	m.getOrCreate(m.httpRequests, key).Add(1)
}

// ObserveHTTPDuration 记录 HTTP 请求耗时。
func (m *Metrics) ObserveHTTPDuration(method, path string, d time.Duration) {
	key := method + "|" + path
	m.getOrCreate(m.httpDurationSum, key).Add(d.Milliseconds())
	m.getOrCreate(m.httpDurationCount, key).Add(1)
}

// IncLLMCall 增加 LLM 调用计数。
func (m *Metrics) IncLLMCall(provider, task, status string) {
	key := provider + "|" + task + "|" + status
	m.getOrCreate(m.llmCalls, key).Add(1)
}

// AddLLMTokens 增加 LLM tokens（n <= 0 时忽略）。
func (m *Metrics) AddLLMTokens(provider, kind string, n int64) {
	if n <= 0 {
		return
	}
	key := provider + "|" + kind
	m.getOrCreate(m.llmTokens, key).Add(n)
}

// IncSSEActive SSE 活跃流 +1。
func (m *Metrics) IncSSEActive() { m.sseActiveStreams.Add(1) }

// DecSSEActive SSE 活跃流 -1。
func (m *Metrics) DecSSEActive() { m.sseActiveStreams.Add(-1) }

// IncCacheHits 增加 cache 命中计数。
func (m *Metrics) IncCacheHits() { m.cacheHitsTotal.Add(1) }

// IncCacheMisses 增加 cache 未命中计数。
func (m *Metrics) IncCacheMisses() { m.cacheMissesTotal.Add(1) }

// IncDBSessions DB sessions +1。
func (m *Metrics) IncDBSessions() { m.dbSessionsActive.Add(1) }

// DecDBSessions DB sessions -1。
func (m *Metrics) DecDBSessions() { m.dbSessionsActive.Add(-1) }

// Snapshot 返回当前所有指标的不可变快照（线程安全）。
type Snapshot struct {
	HTTPRequests      map[string]int64
	HTTPDurationSum   map[string]int64
	HTTPDurationCount map[string]int64
	LLMCalls          map[string]int64
	LLMTokens         map[string]int64
	SSEActiveStreams  int64
	CacheHitsTotal    int64
	CacheMissesTotal  int64
	DBSessionsActive  int64
	UptimeSeconds     float64
	GoRoutines        int
	Version           string
	Commit            string
	GoVersion         string
}

// Snapshot 取当前快照。
func (m *Metrics) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return Snapshot{
		HTTPRequests:      copyInt64Map(m.httpRequests),
		HTTPDurationSum:   copyInt64Map(m.httpDurationSum),
		HTTPDurationCount: copyInt64Map(m.httpDurationCount),
		LLMCalls:          copyInt64Map(m.llmCalls),
		LLMTokens:         copyInt64Map(m.llmTokens),
		SSEActiveStreams:  m.sseActiveStreams.Load(),
		CacheHitsTotal:    m.cacheHitsTotal.Load(),
		CacheMissesTotal:  m.cacheMissesTotal.Load(),
		DBSessionsActive:  m.dbSessionsActive.Load(),
		UptimeSeconds:     time.Since(m.startTime).Seconds(),
		GoRoutines:        runtime.NumGoroutine(),
		Version:           m.version,
		Commit:            m.commit,
		GoVersion:         m.goVersion,
	}
}

func copyInt64Map(src map[string]*atomic.Int64) map[string]int64 {
	out := make(map[string]int64, len(src))
	for k, v := range src {
		out[k] = v.Load()
	}
	return out
}

// PrometheusText 序列化为 Prometheus text format（version 0.0.4）。
//
// 参考：https://prometheus.io/docs/instrumenting/exposition_formats/
func (m *Metrics) PrometheusText() string {
	snap := m.Snapshot()
	var sb strings.Builder

	// novel2all_http_requests_total
	fmt.Fprintf(&sb, "# HELP novel2all_http_requests_total Total number of HTTP requests handled\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_http_requests_total counter\n")
	for _, k := range sortedKeys(snap.HTTPRequests) {
		method, path, status, ok := splitHTTPKey(k)
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "novel2all_http_requests_total{method=%q,path=%q,status=%q} %d\n",
			method, path, status, snap.HTTPRequests[k])
	}

	// novel2all_http_request_duration_seconds（histogram sum + count，简化版）
	fmt.Fprintf(&sb, "# HELP novel2all_http_request_duration_seconds Sum of HTTP request duration in seconds\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_http_request_duration_seconds counter\n")
	for _, k := range sortedKeys(snap.HTTPDurationSum) {
		method, path, ok := splitMethodPathKey(k)
		if !ok {
			continue
		}
		sumSec := float64(snap.HTTPDurationSum[k]) / 1000.0
		fmt.Fprintf(&sb, "novel2all_http_request_duration_seconds_sum{method=%q,path=%q} %.3f\n",
			method, path, sumSec)
	}
	for _, k := range sortedKeys(snap.HTTPDurationCount) {
		method, path, ok := splitMethodPathKey(k)
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "novel2all_http_request_duration_seconds_count{method=%q,path=%q} %d\n",
			method, path, snap.HTTPDurationCount[k])
	}

	// novel2all_llm_calls_total
	fmt.Fprintf(&sb, "# HELP novel2all_llm_calls_total Total number of LLM API calls\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_llm_calls_total counter\n")
	for _, k := range sortedKeys(snap.LLMCalls) {
		provider, task, status, ok := splitLLMCallKey(k)
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "novel2all_llm_calls_total{provider=%q,task=%q,status=%q} %d\n",
			provider, task, status, snap.LLMCalls[k])
	}

	// novel2all_llm_tokens_total
	fmt.Fprintf(&sb, "# HELP novel2all_llm_tokens_total Total LLM tokens consumed\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_llm_tokens_total counter\n")
	for _, k := range sortedKeys(snap.LLMTokens) {
		provider, kind, ok := splitMethodPathKey(k)
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "novel2all_llm_tokens_total{provider=%q,kind=%q} %d\n",
			provider, kind, snap.LLMTokens[k])
	}

	// novel2all_sse_active_streams
	fmt.Fprintf(&sb, "# HELP novel2all_sse_active_streams Current number of active SSE streams\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_sse_active_streams gauge\n")
	fmt.Fprintf(&sb, "novel2all_sse_active_streams %d\n", snap.SSEActiveStreams)

	// novel2all_cache_hits_total / cache_misses_total
	fmt.Fprintf(&sb, "# HELP novel2all_cache_hits_total Total LLM cache hits\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_cache_hits_total counter\n")
	fmt.Fprintf(&sb, "novel2all_cache_hits_total %d\n", snap.CacheHitsTotal)
	fmt.Fprintf(&sb, "# HELP novel2all_cache_misses_total Total LLM cache misses\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_cache_misses_total counter\n")
	fmt.Fprintf(&sb, "novel2all_cache_misses_total %d\n", snap.CacheMissesTotal)

	// novel2all_db_sessions_active
	fmt.Fprintf(&sb, "# HELP novel2all_db_sessions_active Current number of active DB sessions\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_db_sessions_active gauge\n")
	fmt.Fprintf(&sb, "novel2all_db_sessions_active %d\n", snap.DBSessionsActive)

	// novel2all_process_uptime_seconds
	fmt.Fprintf(&sb, "# HELP novel2all_process_uptime_seconds Process uptime in seconds\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_process_uptime_seconds gauge\n")
	fmt.Fprintf(&sb, "novel2all_process_uptime_seconds %.3f\n", snap.UptimeSeconds)

	// novel2all_go_goroutines
	fmt.Fprintf(&sb, "# HELP novel2all_go_goroutines Number of goroutines that currently exist\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_go_goroutines gauge\n")
	fmt.Fprintf(&sb, "novel2all_go_goroutines %d\n", snap.GoRoutines)

	// novel2all_build_info（恒为 1，标签里塞版本信息）
	fmt.Fprintf(&sb, "# HELP novel2all_build_info Build information\n")
	fmt.Fprintf(&sb, "# TYPE novel2all_build_info gauge\n")
	fmt.Fprintf(&sb, "novel2all_build_info{version=%q,commit=%q,go=%q} 1\n",
		snap.Version, snap.Commit, snap.GoVersion)

	return sb.String()
}

// httpKey 拼装 HTTP 计数 key。
func httpKey(method, path string, status int) string {
	return fmt.Sprintf("%s|%s|%d", method, path, status)
}

// splitHTTPKey 拆分 "METHOD|PATH|STATUS"。
func splitHTTPKey(k string) (method, path, status string, ok bool) {
	parts := strings.SplitN(k, "|", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// splitMethodPathKey 拆分 "METHOD|PATH" 或 "A|B"。
func splitMethodPathKey(k string) (a, b string, ok bool) {
	parts := strings.SplitN(k, "|", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// splitLLMCallKey 拆分 "PROVIDER|TASK|STATUS"。
func splitLLMCallKey(k string) (provider, task, status string, ok bool) {
	parts := strings.SplitN(k, "|", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// sortedKeys 排序返回 map keys。
func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
