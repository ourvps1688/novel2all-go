// adaptive_router_test.go 测试 AdaptiveRouter.
package llm

import (
	"context"
	"errors"
	"testing"
)

// fakeDataSource 是测试用的 in-memory data source.
type fakeDataSource struct {
	records map[string]ModelStats // key = task|model
}

func newFakeDataSource() *fakeDataSource {
	return &fakeDataSource{records: make(map[string]ModelStats)}
}

func (f *fakeDataSource) RecordResult(_ context.Context, task, model string, success bool) error {
	key := task + "|" + model
	s := f.records[key]
	s.Task = TaskType(task)
	s.Model = model
	s.Samples++
	if success {
		s.SuccessCount++
	}
	s.SuccessRate = float64(s.SuccessCount) / float64(s.Samples)
	f.records[key] = s
	return nil
}

func (f *fakeDataSource) ListByTask(_ context.Context, task string) ([]ModelStats, error) {
	out := make([]ModelStats, 0)
	for _, s := range f.records {
		if string(s.Task) == task {
			out = append(out, s)
		}
	}
	return out, nil
}

func TestAdaptiveRouter_ColdStart(t *testing.T) {
	src := newFakeDataSource()
	ar := NewAdaptiveRouter(src, StrategyBestAvg, 5)

	model, err := ar.Select(context.Background(), TaskWriting)
	if err != nil {
		t.Errorf("Select error: %v", err)
	}
	if model != "" {
		t.Errorf("cold start should return empty, got %q", model)
	}
}

func TestAdaptiveRouter_SelectBestBySuccessRate(t *testing.T) {
	src := newFakeDataSource()
	ctx := context.Background()

	// Model A: 5/5 = 1.0 success
	for i := 0; i < 5; i++ {
		_ = src.RecordResult(ctx, string(TaskWriting), "minimax-M3", true)
	}
	// Model B: 3/5 = 0.6 success
	for i := 0; i < 3; i++ {
		_ = src.RecordResult(ctx, string(TaskWriting), "deepseek", true)
	}
	for i := 0; i < 2; i++ {
		_ = src.RecordResult(ctx, string(TaskWriting), "deepseek", false)
	}

	ar := NewAdaptiveRouter(src, StrategyBestSuccess, 5)
	model, err := ar.Select(ctx, TaskWriting)
	if err != nil {
		t.Errorf("Select error: %v", err)
	}
	if model != "minimax-M3" {
		t.Errorf("Select should pick minimax-M3 (success_rate 1.0), got %q", model)
	}
}

func TestAdaptiveRouter_MinSamplesThreshold(t *testing.T) {
	src := newFakeDataSource()
	ctx := context.Background()
	// 3/3 = 1.0 success (但 samples=3 < minSamples=5)
	for i := 0; i < 3; i++ {
		_ = src.RecordResult(ctx, string(TaskWriting), "minimax-M3", true)
	}

	ar := NewAdaptiveRouter(src, StrategyBestSuccess, 5)
	model, _ := ar.Select(ctx, TaskWriting)
	if model != "" {
		t.Errorf("samples < minSamples should return empty (cold start), got %q", model)
	}
}

func TestAdaptiveRouter_NoStore(t *testing.T) {
	ar := NewAdaptiveRouter(nil, StrategyBestAvg, 5)
	if _, err := ar.Select(context.Background(), TaskWriting); !errors.Is(err, ErrNoStore) {
		t.Errorf("Select with nil source should return ErrNoStore, got %v", err)
	}
	if err := ar.Record(context.Background(), TaskWriting, "x", true, 0, 0); !errors.Is(err, ErrNoStore) {
		t.Errorf("Record with nil source should return ErrNoStore, got %v", err)
	}
}

func TestAdaptiveRouter_BestAvgStrategy(t *testing.T) {
	src := newFakeDataSource()
	ctx := context.Background()

	// 两个 model 都达到 minSamples, success_rate 都是 1.0
	// best_avg 用 success_rate * 100 排序, 都一样 → 按 Stable sort 选第一个
	for i := 0; i < 5; i++ {
		_ = src.RecordResult(ctx, string(TaskWriting), "minimax-M3", true)
		_ = src.RecordResult(ctx, string(TaskWriting), "deepseek", true)
	}

	ar := NewAdaptiveRouter(src, StrategyBestAvg, 5)
	model, err := ar.Select(ctx, TaskWriting)
	if err != nil {
		t.Errorf("Select error: %v", err)
	}
	if model == "" {
		t.Errorf("expected a model, got empty")
	}
	// 任意一个 (按 sort.Slice 稳定) 都行
	if model != "minimax-M3" && model != "deepseek" {
		t.Errorf("expected minimax-M3 or deepseek, got %q", model)
	}
}
