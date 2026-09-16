package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	h := NewHealthHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际=%d", rec.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON 解析失败：%v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("Status 期望 ok，实际=%q", resp.Status)
	}
	if resp.Timestamp == "" {
		t.Error("Timestamp 不应为空")
	}
}

func TestVersionHandler(t *testing.T) {
	h := NewVersionHandler()
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际=%d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON 解析失败：%v", err)
	}
	if _, ok := resp["version"]; !ok {
		t.Error("response 应包含 version 字段")
	}
}
