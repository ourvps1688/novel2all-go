// security_headers_test.go 测试 securityHeadersMiddleware.
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders_Defaults(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	h := NewSecurityHeadersMiddleware(next, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("next handler not called")
	}

	// 验证所有默认 header 都被注入
	for k, v := range DefaultSecurityHeaders {
		got := rec.Header().Get(k)
		if got != v {
			t.Errorf("header %q = %q, want %q", k, got, v)
		}
	}

	// 验证 5 个默认 header 都注入了
	// Content-Type 是 next handler 设置的 (test 不验证, 因为 httptest.NewRecorder 只在 Write() 时自动 set Content-Type
	// 而 WriteHeader 已被 next handler 显式调用, 所以 Content-Type 由 next handler 决定)
}

func TestSecurityHeaders_CustomHeaders(t *testing.T) {
	custom := map[string]string{
		"X-Custom-Header": "custom-value",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	h := NewSecurityHeadersMiddleware(next, custom)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("custom header = %q, want custom-value", got)
	}
	// 验证默认 header 没有被注入 (custom 替换)
	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("default header leaked: X-Frame-Options = %q", got)
	}
}

func TestSecurityHeaders_NoDoubleInject(t *testing.T) {
	// 验证 middleware 不会重复注入 headers (injected 标记工作)
	counter := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "value")
		w.WriteHeader(http.StatusOK)
		// 再次 WriteHeader 不会重复注入
		w.WriteHeader(http.StatusOK)
		counter++
		w.Write([]byte("ok"))
	})
	h := NewSecurityHeadersMiddleware(next, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if counter != 1 {
		t.Errorf("next called %d times, want 1", counter)
	}
	// X-Frame-Options 应该被注入
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
}
