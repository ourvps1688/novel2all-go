package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// TestSkillsList 测试 GET /api/skills
func TestSkillsList(t *testing.T) {
	loader, _ := skills.NewLoader()
	if loader == nil {
		t.Fatal("loader nil")
	}
	router := llm.NewRouter(llm.LLMConfig{DeepSeekAPIKey: "test"})
	exec := skills.NewExecutor(loader, router)
	h := NewSkillsHandler(exec, loader)

	req := httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际=%d", rec.Code)
	}
	var resp SkillsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON 解析失败：%v", err)
	}
	if resp.Count != 13 {
		t.Errorf("期望 13 个 skill，实际=%d", resp.Count)
	}
}

// TestSkillsExecute_BadRequest 测试缺 input 字段
func TestSkillsExecute_BadRequest(t *testing.T) {
	loader, _ := skills.NewLoader()
	router := llm.NewRouter(llm.LLMConfig{DeepSeekAPIKey: "test"})
	exec := skills.NewExecutor(loader, router)
	h := NewSkillsHandler(exec, loader)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/skills/story/execute", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("缺 input 应返回 400，实际=%d", rec.Code)
	}
}

// TestSkillsExecute_NotFound 测试不存在的 skill
func TestSkillsExecute_NotFound(t *testing.T) {
	loader, _ := skills.NewLoader()
	router := llm.NewRouter(llm.LLMConfig{DeepSeekAPIKey: "test"})
	exec := skills.NewExecutor(loader, router)
	h := NewSkillsHandler(exec, loader)

	body := strings.NewReader(`{"input":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/skills/nonexistent/execute", body)
	rec := httptest.NewRecorder()
	h.executeStream(rec, req, "nonexistent")

	// 应该发 SSE error 事件
	scanner := bufio.NewScanner(rec.Body)
	var foundErr bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			var ev SSEEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &ev); err == nil {
				if ev.Event == "error" {
					foundErr = true
				}
			}
		}
	}
	if !foundErr {
		t.Error("应该发送 error SSE 事件")
	}
}

// TestSkillsExecuteSync_NotFound 测试 sync 路径的 404
func TestSkillsExecuteSync_NotFound(t *testing.T) {
	loader, _ := skills.NewLoader()
	router := llm.NewRouter(llm.LLMConfig{DeepSeekAPIKey: "test"})
	exec := skills.NewExecutor(loader, router)
	h := NewSkillsHandler(exec, loader)

	body := strings.NewReader(`{"input":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/skills/nonexistent/execute-sync", body)
	rec := httptest.NewRecorder()
	h.executeSync(rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际=%d", rec.Code)
	}
}

// TestSendSSE 测试 SSE 事件格式
func TestSendSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	flusher := &flushRecorder{ResponseWriter: rec}

	sendSSE(rec, flusher, SSEEvent{Event: "chunk", Content: "hello"})

	body := rec.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Errorf("SSE 应以 'data: ' 开头，实际=%q", body)
	}
	if !strings.Contains(body, `"event":"chunk"`) {
		t.Errorf("SSE 应包含 event 字段，实际=%q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("SSE 应以 \\n\\n 结尾，实际=%q", body)
	}
}

type flushRecorder struct {
	http.ResponseWriter
	flushed bool
}

func (f *flushRecorder) Flush() {
	f.flushed = true
}
