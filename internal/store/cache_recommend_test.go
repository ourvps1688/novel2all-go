// cache_recommend_test.go 测试 Recommend 函数.
package store

import "testing"

func TestRecommend_BackendJSON(t *testing.T) {
	rec := Recommend(CacheRecommendationStats{
		Backend: "json", MaxSize: 1024, TTLSeconds: 0,
		Hits: 50, Misses: 50, Size: 100, HitRate: 0.5,
	})
	if rec.Recommended["backend"].Recommended != "sqlite" {
		t.Errorf("json → sqlite recommended, got %v", rec.Recommended["backend"].Recommended)
	}
	if rec.HealthScore >= 1.0 {
		t.Errorf("low hit_rate + json backend should reduce health score, got %v", rec.HealthScore)
	}
	if len(rec.Actions) == 0 {
		t.Errorf("expected at least 1 action")
	}
}

func TestRecommend_BackendSQLiteOK(t *testing.T) {
	rec := Recommend(CacheRecommendationStats{
		Backend: "sqlite", MaxSize: 1024, TTLSeconds: 86400,
		Hits: 80, Misses: 20, Size: 500, HitRate: 0.8,
	})
	if rec.Recommended["backend"].Recommended != "sqlite" {
		t.Errorf("already sqlite should stay sqlite, got %v", rec.Recommended["backend"].Recommended)
	}
	if rec.HealthScore != 1.0 {
		t.Errorf("healthy config should have health_score 1.0, got %v", rec.HealthScore)
	}
	if len(rec.Issues) != 0 {
		t.Errorf("no issues expected, got %v", rec.Issues)
	}
}

func TestRecommend_CacheFull(t *testing.T) {
	rec := Recommend(CacheRecommendationStats{
		Backend: "sqlite", MaxSize: 1000, TTLSeconds: 86400,
		Hits: 900, Misses: 100, Size: 1000, HitRate: 0.9,
	})
	if _, ok := rec.Recommended["max_size"]; !ok {
		t.Fatal("max_size recommendation missing")
	}
	newSize := rec.Recommended["max_size"].Recommended
	if newSize == 1000 {
		t.Errorf("full cache should recommend bigger size, got %v", newSize)
	}
}

func TestRecommend_LowHitRate(t *testing.T) {
	rec := Recommend(CacheRecommendationStats{
		Backend: "sqlite", MaxSize: 256, TTLSeconds: 86400,
		Hits: 30, Misses: 70, Size: 256, HitRate: 0.3,
	})
	// low hit_rate + cache full → 应触发 max_size 推荐
	if _, ok := rec.Recommended["max_size"]; !ok {
		t.Fatal("max_size should be recommended")
	}
	if rec.HealthScore > 0.7 {
		t.Errorf("low hit_rate should reduce health score significantly, got %v", rec.HealthScore)
	}
}

func TestRecommend_ConfidenceBySamples(t *testing.T) {
	// < 10 samples → low
	r1 := Recommend(CacheRecommendationStats{Backend: "sqlite", Hits: 3, Misses: 2})
	if r1.Confidence != "low" {
		t.Errorf("5 samples should be low, got %s", r1.Confidence)
	}
	// 10-99 → medium
	r2 := Recommend(CacheRecommendationStats{Backend: "sqlite", Hits: 30, Misses: 20})
	if r2.Confidence != "medium" {
		t.Errorf("50 samples should be medium, got %s", r2.Confidence)
	}
	// >= 100 → high
	r3 := Recommend(CacheRecommendationStats{Backend: "sqlite", Hits: 80, Misses: 20})
	if r3.Confidence != "high" {
		t.Errorf("100 samples should be high, got %s", r3.Confidence)
	}
}

func TestRecommend_NoTTL(t *testing.T) {
	rec := Recommend(CacheRecommendationStats{
		Backend: "sqlite", MaxSize: 1024, TTLSeconds: 0,
		Hits: 80, Misses: 20, HitRate: 0.8,
	})
	if rec.Recommended["ttl_seconds"].Recommended != 86400 {
		t.Errorf("TTL=0 should recommend 86400, got %v", rec.Recommended["ttl_seconds"].Recommended)
	}
}
