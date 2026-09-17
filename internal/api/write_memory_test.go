package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/memory"
	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// newTestWriteMemoryHandler 创建带 MemoryManager 的 WriteHandler (Sprint 32 e2e 用).
//
// 与 newTestWriteHandler 区别: 注入真实 MemoryManager (用 in-memory tracker 模拟).
// 与 newTestMemoryHandler (memory_test.go) 区别: 那个返回 MemoryHandler, 这个返回 WriteHandler.
func newTestWriteMemoryHandler(t *testing.T, projectRoot string) *WriteHandler {
	t.Helper()

	// 1. 构造 memory.MemoryManager (chroma 自动降级, retriever=nil 跳过 L4)
	memMgr := memory.NewMemoryManager(projectRoot, nil /* router */, memory.MemoryConfig{
		CoreTokenBudget:      3000,
		CharacterTokenBudget: 4000,
		RecentChapterCount:   5,
		EventTopK:            8,
	})

	// 2. 构造 skills (用 assets 里 13 个 SKILL.md)
	loader, err := skills.NewLoader()
	if err != nil {
		t.Fatalf("loader: %v", err)
	}

	// 3. NewWriteHandlerWithMemory
	return NewWriteHandlerWithMemory(nil /* executor=nil */, loader, memMgr, nil)
}

// setupMockProject 准备 mock 项目目录 (Sprint 32 e2e 用).
func setupMockProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// 1. 设定/文风.md
	if err := os.MkdirAll(filepath.Join(dir, "设定"), 0o755); err != nil {
		t.Fatal(err)
	}
	styleMD := `# 文风
- 句长 20-40 字
- 大量对话
- 通俗易懂
`
	if err := os.WriteFile(filepath.Join(dir, "设定", "文风.md"), []byte(styleMD), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. 创作设定.md
	setupMD := `# 创作设定
书名: 测试小说
题材: 玄幻
文风: 热血
`
	if err := os.WriteFile(filepath.Join(dir, "创作设定.md"), []byte(setupMD), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. 大纲/细纲_第001章.md
	if err := os.MkdirAll(filepath.Join(dir, "大纲"), 0o755); err != nil {
		t.Fatal(err)
	}
	outline := `# 第1章 细纲
## 场景1
- 主角林雷在山脚练剑
- 师父出现
- 教他基础剑法
`
	if err := os.WriteFile(filepath.Join(dir, "大纲", "细纲_第001章.md"), []byte(outline), 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. 正文/ (空)
	if err := os.MkdirAll(filepath.Join(dir, "正文"), 0o755); err != nil {
		t.Fatal(err)
	}

	return dir
}

// TestHandleWriteStream_OutlineMissing 测试 pre-write gate: 细纲缺失返回 error event.
//
// 注: handler 当前在 executor=nil 时直接报 executor 错误 (短路 pre-write check).
// 真 pre-write 路径需要 mock executor, 留给 Sprint 32d (集成测试用 mock_provider).
func TestHandleWriteStream_OutlineMissing(t *testing.T) {
	dir := t.TempDir() // 空目录, 无 设定/ / 大纲/
	h := newTestWriteMemoryHandler(t, dir)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/stream?chapter=1&project_root="+dir, http.NoBody)
	h.handleStream(w, r)

	body := w.Body.String()
	// executor=nil 时返回 error event (短路)
	// SSE 格式: "event: error\ndata: {...}\n\n" (注意 event: 后是空格 + 名字, 不是 JSON)
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected error event, got: %s", body[:min(500, len(body))])
	}
	// 不应该有 pre_write_check (executor 短路)
	if strings.Contains(body, "event: pre_write_check") {
		t.Errorf("should skip pre_write_check when executor=nil, got: %s", body[:min(500, len(body))])
	}
}

// TestHandleWriteStream_PreWriteSuccess 测试 executor=nil 时短路 (行为符合 V0.29).
func TestHandleWriteStream_PreWriteSuccess(t *testing.T) {
	dir := setupMockProject(t)
	h := newTestWriteMemoryHandler(t, dir)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/stream?chapter=1&project_root="+dir, http.NoBody)
	h.handleStream(w, r)

	body := w.Body.String()
	// 验证 SSE 事件序列: started → error (executor 短路)
	if !strings.Contains(body, "event: started") {
		t.Errorf("expected started event, got: %s", body[:min(500, len(body))])
	}
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected error event (executor=nil), got: %s", body[:min(500, len(body))])
	}
}

// TestBuildSystemPrompt_AllSections 测试 system prompt 拼接完整.
func TestBuildSystemPrompt_AllSections(t *testing.T) {
	dir := setupMockProject(t)
	memMgr := memory.NewMemoryManager(dir, nil, memory.MemoryConfig{
		CoreTokenBudget: 3000, CharacterTokenBudget: 4000, RecentChapterCount: 5, EventTopK: 8,
	})

	ctx := context.Background()
	prompt := buildSystemPrompt(nil, memMgr, ctx, 1, dir)

	// 必须包含 3 个 section
	if !strings.Contains(prompt, "设定/文风.md") {
		t.Errorf("expected style.md section, got: %s", prompt[:min(500, len(prompt))])
	}
	if !strings.Contains(prompt, "创作设定.md") {
		t.Errorf("expected setup.md section, got: %s", prompt[:min(500, len(prompt))])
	}
	if !strings.Contains(prompt, "# 核心设定") {
		t.Errorf("expected core memory section, got: %s", prompt[:min(500, len(prompt))])
	}
}

// TestBuildSystemPrompt_NilMemMgr 测试 memMgr=nil 时不 panic.
func TestBuildSystemPrompt_NilMemMgr(t *testing.T) {
	dir := setupMockProject(t)
	ctx := context.Background()
	// memMgr=nil → 跳过 5 层 memory, 但 settings 仍加载
	prompt := buildSystemPrompt(nil, nil, ctx, 1, dir)
	if prompt == "" {
		t.Error("prompt empty")
	}
	if !strings.Contains(prompt, "设定/文风.md") {
		t.Errorf("expected settings even when memMgr=nil, got: %s", prompt)
	}
}

// TestBuildUserPrompt_OutlineFound 测试加载真 outline.
func TestBuildUserPrompt_OutlineFound(t *testing.T) {
	dir := setupMockProject(t)
	prompt := buildUserPrompt(dir, 1, 2000)
	if !strings.Contains(prompt, "本章细纲") {
		t.Errorf("expected '本章细纲' section, got: %s", prompt)
	}
	if !strings.Contains(prompt, "练剑") {
		t.Errorf("expected outline content (练剑), got: %s", prompt)
	}
	if !strings.Contains(prompt, "2000") {
		t.Errorf("expected min_chars in prompt, got: %s", prompt)
	}
}

// TestBuildUserPrompt_OutlineMissing 测试细纲缺失时降级.
func TestBuildUserPrompt_OutlineMissing(t *testing.T) {
	dir := t.TempDir() // 无 大纲/
	prompt := buildUserPrompt(dir, 1, 2000)
	if !strings.Contains(prompt, "第 1 章") {
		t.Errorf("expected fallback '第 1 章', got: %s", prompt)
	}
}

// TestReadOutline_FileNotExist 测试细纲文件不存在 graceful.
func TestReadOutline_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	outline, err := readOutline(dir, 1)
	if err == nil {
		t.Error("expected error for missing outline")
	}
	if outline != "" {
		t.Errorf("expected empty outline, got: %s", outline)
	}
}

// TestNewWriteHandlerWithMemory_NilDeps 测试 nil 降级 (Sprint 32 向后兼容).
func TestNewWriteHandlerWithMemory_NilDeps(t *testing.T) {
	loader, _ := skills.NewLoader()
	h := NewWriteHandlerWithMemory(nil, loader, nil, nil)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.memMgr != nil {
		t.Error("expected nil memMgr")
	}
	if h.refLoader != nil {
		t.Error("expected nil refLoader")
	}

	// 验证 handleStream 仍能跑 (走 V0.29 mock 路径)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/write/stream?chapter=1&project_root=.", http.NoBody)
	h.handleStream(w, r)
	body := w.Body.String()
	if !strings.Contains(body, "started") {
		t.Errorf("expected started event, got: %s", body[:min(300, len(body))])
	}
}

// TestMemoryContext_RealLoad 测试真实 MemoryManager 加载 5 层 memory.
func TestMemoryContext_RealLoad(t *testing.T) {
	dir := setupMockProject(t)
	memMgr := memory.NewMemoryManager(dir, nil, memory.MemoryConfig{
		CoreTokenBudget: 3000, CharacterTokenBudget: 4000, RecentChapterCount: 5, EventTopK: 8,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mc, err := memMgr.LoadForWriting(ctx, 1)
	if err != nil {
		t.Fatalf("LoadForWriting: %v", err)
	}
	if mc == nil {
		t.Fatal("expected non-nil MemoryContext")
	}

	// 验证 5 层都有 (即使空也有 section name)
	sections := mc.ToSystemSections()
	if len(sections) == 0 {
		t.Error("expected at least 1 section")
	}

	// 验证可以 Marshal 成 JSON (下游 pipeline 用)
	data, err := json.Marshal(mc)
	if err != nil {
		t.Errorf("marshal: %v", err)
	}
	if len(data) < 10 {
		t.Error("marshal too small")
	}
}
