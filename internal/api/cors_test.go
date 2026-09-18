package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// corsTestNext 是测试用 next handler, 返回 200 + 标识 body.
func corsTestNext() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

// TestCORS_Preflight_OPTIONS_Returns204 验证: OPTIONS preflight → 204 + Allow-* headers + Max-Age.
func TestCORS_Preflight_OPTIONS_Returns204(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowOrigins = []string{"https://example.com"}
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	req := httptest.NewRequest(http.MethodOptions, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight 应返回 204, 实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Allow-Origin=%q, want https://example.com", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Allow-Methods 不应为空")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Allow-Headers 不应为空")
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("Max-Age=%q, want 86400", got)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("preflight body 应为空, got=%q", rec.Body.String())
	}
}

// TestCORS_Preflight_DoesNotCallNext 验证: preflight 短路, next 不被调用.
//
// 防止 next (logging / mux) 看到 OPTIONS 请求, 污染 metrics.
func TestCORS_Preflight_DoesNotCallNext(t *testing.T) {
	cfg := DefaultCORSConfig()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := NewCORSMiddleware(next, cfg)

	req := httptest.NewRequest(http.MethodOptions, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight 应 204, got %d", rec.Code)
	}
	if called {
		t.Error("preflight 不应调 next")
	}
}

// TestCORS_GET_HeadersInjected 验证: 普通 GET 请求 → next 调用 + CORS 头注入.
func TestCORS_GET_HeadersInjected(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowOrigins = []string{"https://example.com"}
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET 应 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Errorf("body 应原样透传, got=%q", rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Allow-Origin=%q, want https://example.com", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != boolStrTrue {
		t.Errorf("Allow-Credentials=%q, want true", got)
	}
}

// TestCORS_GET_NoMaxAge 验证: 非 preflight 不写 Max-Age 头.
func TestCORS_GET_NoMaxAge(t *testing.T) {
	cfg := DefaultCORSConfig()
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Max-Age"); got != "" {
		t.Errorf("非 preflight 不应写 Max-Age, got=%q", got)
	}
}

// TestCORS_WildcardCreds_EchoesOrigin 验证: AllowOrigins=["*"] + AllowCreds=true
// 时, Allow-Origin 回显请求 Origin (而非字面 "*"), 规避浏览器 reject.
func TestCORS_WildcardCreds_EchoesOrigin(t *testing.T) {
	cfg := DefaultCORSConfig() // AllowOrigins=["*"], AllowCreds=true
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://app.ourvps1688.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("应 200, got %d", rec.Code)
	}
	got := rec.Header().Get("Access-Control-Allow-Origin")
	if got == "*" {
		t.Error("Wildcard + Creds 时不应返回字面 '*', 浏览器会 reject")
	}
	if got != "https://app.ourvps1688.com" {
		t.Errorf("Allow-Origin=%q, want 回显 origin", got)
	}
	// 回显 origin 时应加 Vary: Origin 防缓存污染
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary=%q, want Origin", got)
	}
}

// TestCORS_WildcardNoCreds_LiteralStar 验证: AllowOrigins=["*"] + AllowCreds=false
// 时, Allow-Origin 保留字面 "*".
func TestCORS_WildcardNoCreds_LiteralStar(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{"Content-Type"},
		AllowCreds:   false,
		MaxAge:       3600,
	}
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://app.ourvps1688.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("应 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Wildcard + NoCreds 应保留 '*', got=%q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("AllowCreds=false 不应写 Allow-Credentials, got=%q", got)
	}
}

// TestCORS_SpecificOrigin_NoMatch 验证: origin 不在白名单 → next 调但不写 CORS 头.
//
// 浏览器会因缺 CORS 头 reject; 服务器侧仍正常处理 (可能返回 401/200, 浏览器拦截).
func TestCORS_SpecificOrigin_NoMatch(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins: []string{"https://allowed.com"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{"Content-Type"},
		AllowCreds:   false,
		MaxAge:       3600,
	}
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("next 应正常返回 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("不应注入 Allow-Origin, got=%q", got)
	}
}

// TestCORS_NoOriginHeader_SkipCORS 验证: 无 Origin 头 (same-origin / 非浏览器)
// → 直接调 next, 不写 CORS 头, 不污染 same-origin 响应.
func TestCORS_NoOriginHeader_SkipCORS(t *testing.T) {
	cfg := DefaultCORSConfig()
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	// 不设 Origin 头
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("应 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("无 Origin 不应写 CORS 头, got=%q", got)
	}
}

// TestCORS_MultipleSpecificOrigins 验证: 多 origin 白名单, 各自匹配.
func TestCORS_MultipleSpecificOrigins(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins: []string{"https://a.com", "https://b.com"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{"Content-Type"},
		AllowCreds:   true,
		MaxAge:       3600,
	}
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	for _, origin := range []string{"https://a.com", "https://b.com"} {
		req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("origin=%q 时 Allow-Origin=%q, want %q", origin, got, origin)
		}
	}

	// 未在白名单的 origin
	req := httptest.NewRequest(http.MethodGet, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://c.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("非白名单 origin 不应写 CORS 头, got=%q", got)
	}
}

// TestCORS_ZeroConfig_AppliesDefaults 验证: 零值 CORSConfig → 自动套用切片/Map/MaxAge 默认.
//
// 注意：AllowCreds 是 bool, 零值 false 不被改 — 调用方应显式用 DefaultCORSConfig() 设 true.
func TestCORS_ZeroConfig_AppliesDefaults(t *testing.T) {
	handler := NewCORSMiddleware(corsTestNext(), CORSConfig{})

	req := httptest.NewRequest(http.MethodOptions, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://any.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("零值 config 应仍正常工作, got status %d", rec.Code)
	}
	// 零值 + Wildcard + AllowCreds=false → Allow-Origin 应保留字面 "*"
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("零值 config (Wildcard + NoCreds) Allow-Origin=%q, want *", got)
	}
	// 零值 config 不应写 Allow-Credentials (AllowCreds 零值 false)
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("零值 config 不应写 Allow-Credentials, got=%q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("零值 config 应有默认 MaxAge=86400, got=%q", got)
	}
}

// TestCORS_PreflightCustomMaxAge 验证: 自定义 MaxAge 在 preflight 生效.
func TestCORS_PreflightCustomMaxAge(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{"Content-Type"},
		AllowCreds:   false,
		MaxAge:       600, // 10 min
	}
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	req := httptest.NewRequest(http.MethodOptions, "/api/projects", http.NoBody)
	req.Header.Set("Origin", "https://any.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight 应 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("Max-Age=%q, want 600", got)
	}
}

// TestCORS_POST_WithCredentials 验证: POST 请求带 cookie + Origin → Allow-Credentials 注入.
//
// 模拟浏览器 cross-origin POST with credentials (cookies).
func TestCORS_POST_WithCredentials(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowOrigins = []string{"https://app.com"}
	handler := NewCORSMiddleware(corsTestNext(), cfg)

	body := []byte(`{"name":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	req.Header.Set("Origin", "https://app.com")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-tok"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("应 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.com" {
		t.Errorf("Allow-Origin=%q, want https://app.com", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != boolStrTrue {
		t.Errorf("Allow-Credentials=%q, want true", got)
	}
}
