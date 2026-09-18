// Package api 提供 novel2all-go HTTP handlers.
//
// cors.go 实现 Sprint V1.0.1 P4: CORS (Cross-Origin Resource Sharing) middleware.
//
// 设计目的:
//   - 让浏览器 SPA (React / Vue) 能从不同 origin 调 API
//   - 处理 OPTIONS preflight 请求, 不进入 logging / metrics chain
//   - 默认 AllowOrigins=["*"] + AllowCreds=true 时, 强制回显 Origin (而非返回 "*"),
//     因为浏览器对 "wildcard + credentials" 组合直接 reject
//
// 用法 (server.go Commit 2):
//
//	handler := api.LoggingMiddleware(logger, metrics, traces, mux)
//	handler = api.NewSecurityHeadersMiddleware(handler, nil)
//	handler = api.NewCORSMiddleware(handler, api.DefaultCORSConfig())
//
// 参考:
//   - https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS
//   - https://fetch.spec.whatwg.org/#http-cors-protocol
package api

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSConfig 配置 CORS 行为.
//
// 零值不安全 — 用 DefaultCORSConfig() 构造.
type CORSConfig struct {
	// AllowOrigins 允许的 origin 列表. 支持:
//   - 精确匹配: "https://example.com"
//   - 通配符:   "*" (允许所有 origin; 与 AllowCreds=true 组合时会强制回显 Origin)
// 默认: ["*"]
	AllowOrigins []string

	// AllowMethods 允许的 HTTP method 列表 (用于 preflight Allow-Methods 头).
	// 默认: ["GET","POST","PUT","DELETE","OPTIONS"]
	AllowMethods []string

	// AllowHeaders 允许的 request header 列表 (用于 preflight Allow-Headers 头).
	// 默认: ["Content-Type","Authorization","Cookie"]
	AllowHeaders []string

	// AllowCreds 是否允许 credentials (cookie / authorization header).
	// true 时回写 Access-Control-Allow-Credentials: true.
	// 默认: true
	AllowCreds bool

	// MaxAge preflight 结果缓存秒数 (回写 Access-Control-Max-Age 头).
	// 仅对 OPTIONS preflight 响应生效.
	// 默认: 86400 (24h)
	MaxAge int
}

// DefaultCORSConfig 返回默认配置 (兼容浏览器 SPA + cookie session).
//
// 当前默认 AllowOrigins=["*"] + AllowCreds=true — 见 NewCORSMiddleware 实现:
// 当 AllowOrigins 含 "*" 且 AllowCreds=true 时, 强制把 Allow-Origin
// 回显为请求的 Origin 头值, 而不是字面 "*" (规避浏览器 reject).
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{"Content-Type", "Authorization", "Cookie"},
		AllowCreds:   true,
		MaxAge:       86400,
	}
}

// NewCORSMiddleware 创建 CORS middleware.
//
// 行为:
//   - 请求无 Origin 头 (same-origin / 非浏览器): 直接 next.ServeHTTP, 不注入 CORS 头
//   - Origin 不在白名单: 直接 next.ServeHTTP, 不注入 CORS 头 (浏览器会自动 reject)
//   - Origin 匹配: 注入 Access-Control-Allow-* 头
//     - AllowOrigins 含 "*" 且 AllowCreds=true: 回显请求 Origin (而非 "*")
//     - 否则按匹配规则返回精确 origin 或 "*"
//   - OPTIONS 方法 (preflight): 额外写 Max-Age 头 + 返回 204 No Content, 不调 next
//   - 其他方法: 调 next.ServeHTTP (CORS 头已注入, 由 next 写 body)
//
// 注意: CORS 必须放在 middleware chain 最外层, 让 OPTIONS preflight 短路不进入
// logging / metrics, 避免日志被 prefight 请求淹没.
func NewCORSMiddleware(next http.Handler, cfg CORSConfig) http.Handler {
	// 应用默认值
	if len(cfg.AllowOrigins) == 0 {
		cfg.AllowOrigins = []string{"*"}
	}
	if len(cfg.AllowMethods) == 0 {
		cfg.AllowMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}
	}
	if len(cfg.AllowHeaders) == 0 {
		cfg.AllowHeaders = []string{"Content-Type", "Authorization", "Cookie"}
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 86400
	}

	allowOriginSet := make(map[string]struct{}, len(cfg.AllowOrigins))
	hasWildcard := false
	for _, o := range cfg.AllowOrigins {
		if o == "*" {
			hasWildcard = true
		}
		allowOriginSet[o] = struct{}{}
	}

	methods := strings.Join(cfg.AllowMethods, ", ")
	headers := strings.Join(cfg.AllowHeaders, ", ")
	maxAge := strconv.Itoa(cfg.MaxAge)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Same-origin 或非浏览器请求, 不需要 CORS 头
			next.ServeHTTP(w, r)
			return
		}

		// 计算允许的 origin 值
		allowed := matchCORSOrigin(origin, allowOriginSet, hasWildcard, cfg.AllowCreds)
		if allowed == "" {
			// Origin 不在白名单 — 不写 CORS 头 (浏览器会拒绝)
			next.ServeHTTP(w, r)
			return
		}

		// 设置 CORS 响应头 (在 next 写入 response body 之前)
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", allowed)
		h.Set("Access-Control-Allow-Methods", methods)
		h.Set("Access-Control-Allow-Headers", headers)
		if cfg.AllowCreds {
			h.Set("Access-Control-Allow-Credentials", "true")
		}
		// 当回显 origin (而不是通配符 *) 时, 加 Vary: Origin 防止 CDN/缓存错误
		if allowed != "*" {
			h.Add("Vary", "Origin")
		}

		// Preflight: OPTIONS 请求直接 204 短路
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Max-Age", maxAge)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// 非 preflight: 调 next, CORS 头已注入
		next.ServeHTTP(w, r)
	})
}

// matchCORSOrigin 计算 Access-Control-Allow-Origin 头值.
//
// 返回值:
//   - "" 表示 origin 不被允许 (调用方应不写 CORS 头)
//   - "*" 表示通配符 (仅当 AllowCreds=false 时合法)
//   - 否则返回请求的 origin (精确匹配或 wildcard+creds 回显)
//
// 规则:
//  1. origin 在白名单中 (精确匹配) → 返回 origin
//  2. 白名单含 "*" 且 AllowCreds=false → 返回 "*"
//  3. 白名单含 "*" 且 AllowCreds=true → 返回 origin (回显, 规避浏览器 reject)
//  4. 都不满足 → 返回 ""
func matchCORSOrigin(origin string, allowSet map[string]struct{}, hasWildcard, allowCreds bool) string {
	if _, ok := allowSet[origin]; ok {
		return origin
	}
	if hasWildcard {
		if allowCreds {
			// Wildcard + Credentials: 浏览器会 reject "*", 必须回显 origin
			return origin
		}
		return "*"
	}
	return ""
}
