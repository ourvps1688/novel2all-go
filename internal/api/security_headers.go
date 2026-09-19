// Package api 提供 novel2all-go HTTP handlers.
package api

// security_headers.go 实现 OWASP 推荐的安全 HTTP headers 中间件.
//
// 设计:
//   - 添加 X-Content-Type-Options (防 MIME sniffing)
//   - 添加 X-Frame-Options (防 clickjacking)
//   - 添加 Referrer-Policy (防 URL 泄漏)
//   - 添加 Permissions-Policy (限制浏览器特性)
//   - 添加 Content-Security-Policy (防 XSS)
//
// 参考 Python V1.0 GA core/security_headers.py.
// 默认 headers 是常量, 调用方可在 NewSecurityHeadersMiddleware 时覆盖.
import "net/http"

// DefaultSecurityHeaders OWASP 推荐的安全 headers.
//
// 注: CSP script-src 'unsafe-inline' 是为了兼容 V1.5 React index.html 内联 FOUC 防护脚本.
// 如未来不再需要可移除.
var DefaultSecurityHeaders = map[string]string{
	"X-Content-Type-Options": "nosniff",
	"X-Frame-Options":        "DENY",
	"Referrer-Policy":        "strict-origin-when-cross-origin",
	"Permissions-Policy": "geolocation=(), microphone=(), camera=(), payment=(), " +
		"usb=(), magnetometer=(), gyroscope=(), accelerometer=()",
	"Content-Security-Policy": "default-src 'self'; " +
		"img-src 'self' data:; " +
		"style-src 'self' 'unsafe-inline'; " +
		"script-src 'self' 'unsafe-inline'; " +
		"connect-src 'self'; " +
		"font-src 'self' data:; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self';",
}

// securityHeadersMiddleware 包装 next, 给每个 Response 添加安全 headers.
type securityHeadersMiddleware struct {
	headers map[string]string
	next    http.Handler
}

// NewSecurityHeadersMiddleware 创建 middleware.
//
// headers 为 nil 时用 DefaultSecurityHeaders.
func NewSecurityHeadersMiddleware(next http.Handler, headers map[string]string) http.Handler {
	if headers == nil {
		headers = DefaultSecurityHeaders
	}
	return &securityHeadersMiddleware{headers: headers, next: next}
}

func (m *securityHeadersMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Wrap ResponseWriter 拦截 WriteHeader 调用, 在状态码写入前注入 headers
	sw := &securityHeaderWriter{ResponseWriter: w, headers: m.headers}
	m.next.ServeHTTP(sw, r)
}

// securityHeaderWriter 包装 http.ResponseWriter, 在 WriteHeader 调用时
// 先注入安全 headers (必须在 WriteHeader 之前, 否则 header 不会发送).
type securityHeaderWriter struct {
	http.ResponseWriter
	headers  map[string]string
	injected bool
}

// WriteHeader 注入 headers 然后写状态码.
//
// 注: 必须先保留原始 Header (Set 不覆盖已存在的). 默认用 Set 但先 Check 避免覆盖.
func (w *securityHeaderWriter) WriteHeader(code int) {
	if !w.injected {
		h := w.Header()
		for k, v := range w.headers {
			if h.Get(k) == "" {
				h.Set(k, v)
			}
		}
		w.injected = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write 触发隐式 WriteHeader, 也会注入 headers.
func (w *securityHeaderWriter) Write(b []byte) (int, error) {
	if !w.injected {
		h := w.Header()
		for k, v := range w.headers {
			if h.Get(k) == "" {
				h.Set(k, v)
			}
		}
		w.injected = true
	}
	return w.ResponseWriter.Write(b)
}
