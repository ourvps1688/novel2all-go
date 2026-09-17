package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCacheStatsHandler(t *testing.T) {
	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/stats/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var s CacheStats
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	if s.Backend != "memory" {
		t.Errorf("backend=%q", s.Backend)
	}
}

func TestCachePromptStatsHandler(t *testing.T) {
	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/prompt-stats/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestCacheMigrate_Success(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src.json"
	dst := dir + "/dst.json"

	// 写 src
	entries := []cacheEntry{
		{Key: "k1", Value: "v1"},
		{Key: "k2", Value: "v2"},
	}
	if err := writeCacheJSON(src, entries, 100, 3600); err != nil {
		t.Fatalf("writeCacheJSON: %v", err)
	}

	h := NewCacheHandler()
	form := url.Values{}
	form.Set("src", src)
	form.Set("dst", dst)
	form.Set("max_size", "200")
	form.Set("ttl_seconds", "7200")
	req := httptest.NewRequest(http.MethodPost, "/api/cache/migrate/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var r MigrateResult
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Migrated != 2 {
		t.Errorf("migrated=%d, want 2", r.Migrated)
	}
	if r.SrcBackend != "json" || r.DstBackend != "json" {
		t.Errorf("backends: src=%s dst=%s", r.SrcBackend, r.DstBackend)
	}
	if r.DstMaxSize != 200 || r.DstTTLSeconds != 7200 {
		t.Errorf("dst params: max=%d ttl=%d", r.DstMaxSize, r.DstTTLSeconds)
	}
	// 验证 dst 文件确实写入了
	readBack, err := readCacheJSON(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if len(readBack) != 2 {
		t.Errorf("dst entries=%d, want 2", len(readBack))
	}
}

func TestCacheMigrate_MissingParams(t *testing.T) {
	h := NewCacheHandler()
	form := url.Values{}
	form.Set("src", "/tmp/src.json")
	// 缺 dst
	req := httptest.NewRequest(http.MethodPost, "/api/cache/migrate/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestCacheMigrate_SrcNotExist(t *testing.T) {
	dir := t.TempDir()
	h := NewCacheHandler()
	form := url.Values{}
	form.Set("src", dir+"/nonexistent.json")
	form.Set("dst", dir+"/dst.json")
	req := httptest.NewRequest(http.MethodPost, "/api/cache/migrate/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// src 不存在应该返回空 entries 而非错误（tolerate empty migration）
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var r MigrateResult
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Migrated != 0 {
		t.Errorf("migrated=%d, want 0", r.Migrated)
	}
}

func TestCacheMigrate_WrongMethod(t *testing.T) {
	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/migrate/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d, want 405", rec.Code)
	}
}

func TestCacheRecommend_NoData(t *testing.T) {
	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/recommend/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var r RecommendResult
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Confidence != "low" {
		t.Errorf("confidence=%s, want low (no data)", r.Confidence)
	}
	if r.HealthScore >= 1.0 {
		t.Errorf("health_score=%f, want < 1.0 (no data)", r.HealthScore)
	}
}

func TestCacheRecommend_GoodConfig(t *testing.T) {
	// 模拟 good config (1200 总请求, 83% hit rate → confidence=high)
	atomic.StoreInt64(&cacheHits, 1000)
	atomic.StoreInt64(&cacheMisses, 200)
	atomic.StoreInt64(&cacheSize, 100)
	atomic.StoreInt64(&cacheMaxSize, 1000)
	atomic.StoreInt64(&cacheTTLSec, 3600)
	defer atomic.StoreInt64(&cacheHits, 0)
	defer atomic.StoreInt64(&cacheMisses, 0)
	defer atomic.StoreInt64(&cacheSize, 0)
	defer atomic.StoreInt64(&cacheMaxSize, 0)
	defer atomic.StoreInt64(&cacheTTLSec, 0)

	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/recommend/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var r RecommendResult
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Confidence != "high" {
		t.Errorf("confidence=%s, want high", r.Confidence)
	}
	if len(r.Issues) > 0 {
		t.Errorf("good config should have no issues: %+v", r.Issues)
	}
}

func TestCacheRecommend_LowHitRate(t *testing.T) {
	atomic.StoreInt64(&cacheHits, 5)
	atomic.StoreInt64(&cacheMisses, 95) // 5% hit rate
	defer atomic.StoreInt64(&cacheHits, 0)
	defer atomic.StoreInt64(&cacheMisses, 0)

	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/recommend/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var r RecommendResult
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if len(r.Issues) == 0 {
		t.Error("low hit rate should produce issues")
	}
	if len(r.Actions) == 0 {
		t.Error("low hit rate should produce actions")
	}
}

func TestCacheRecommend_WrongMethod(t *testing.T) {
	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/cache/recommend/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d, want 405", rec.Code)
	}
}

func TestCacheReset(t *testing.T) {
	// 先设置一些 hits
	atomic.StoreInt64(&cacheHits, 100)
	atomic.StoreInt64(&cacheMisses, 50)
	atomic.StoreInt64(&cacheSize, 75)

	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/cache/reset/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var r map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r["reset"] != true {
		t.Errorf("reset=%v", r["reset"])
	}
	// 验证 counters 已重置
	if atomic.LoadInt64(&cacheHits) != 0 {
		t.Error("hits not reset")
	}
	if atomic.LoadInt64(&cacheMisses) != 0 {
		t.Error("misses not reset")
	}
	if atomic.LoadInt64(&cacheSize) != 0 {
		t.Error("size not reset")
	}
}

func TestCacheReset_WrongMethod(t *testing.T) {
	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/reset/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d, want 405", rec.Code)
	}
}

func TestDetectBackend(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/tmp/cache.json", "json"},
		{"/tmp/cache.db", "sqlite"},
		{"/tmp/cache.sqlite", "sqlite"},
		{"redis://localhost:6379", "redis"},
		{"/tmp/cache.bin", "unknown"},
	}
	for _, c := range cases {
		if got := detectBackend(c.path); got != c.want {
			t.Errorf("detectBackend(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestCacheReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cache.json"
	entries := []cacheEntry{
		{Key: "a", Value: "1"},
		{Key: "b", Value: "2"},
	}
	if err := writeCacheJSON(path, entries, 100, 3600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readCacheJSON(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("entries=%d, want 2", len(got))
	}
	if got[0].Key != "a" || got[0].Value != "1" {
		t.Errorf("entry[0]=%+v", got[0])
	}
}

func TestCacheUnknownAction(t *testing.T) {
	h := NewCacheHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/unknown/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

// 移除可能存在的 cache.json
func TestMain(m *testing.M) {
	_ = os.Remove("data/cache.json")
	os.Exit(m.Run())
}
