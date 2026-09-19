// Package api 提供 novel2all-go HTTP handlers.
//
// auth_jwt.go Phase 2 桌面 app 鉴权扩展.
//
// 设计:
//   - 桌面 app 用 Authorization: Bearer <token> 头, 不依赖 cookie
//   - token 与现有 cookie session 共享 sessions 表 (单一权威来源)
//   - 不引入 JWT 库 (golang-jwt), 复用现有 SessionManager.Login 返的 token
//   - 通过 extractToken() 自动支持 Cookie + Bearer (auth_middleware.go 已实现)
//
// 桌面 app 流程:
//  1. POST /api/auth/login {username, password}
//  2. 返 {access_token, expires_in, username, role}
//  3. 后续请求: Authorization: Bearer <access_token>
//  4. 后端 GetUserByToken 通过 extractToken 解析 (Cookie 或 Bearer)
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// LoginJWTRequest POST /api/auth/login (Phase 2 桌面 app 用).
type LoginJWTRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginJWTResponse POST /api/auth/login 响应.
type LoginJWTResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // 秒
	TokenType   string `json:"token_type"` // 固定 "Bearer"
	Username    string `json:"username"`
	Role        string `json:"role"`
}

// MeResponse GET /api/auth/me 响应.
type MeResponse struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// loginJWTHandler POST /api/auth/login (Phase 2 桌面 app Bearer token 版).
//
// 与现有 cookie 版差异:
//   - 不写 Set-Cookie
//   - 返 JSON {access_token, expires_in, username, role}
//   - Bearer 客户端 (桌面 app) 把 token 存到 %APPDATA%\novel2all-desktop\token
//
// 与 cookie 版共享限流 + 审计 + bcrypt 验证逻辑 (调 h.manager.Login).
func (h *AuthHandler) loginJWT(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	ua := r.UserAgent()

	var req LoginJWTRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSONError(w, "username and password required", http.StatusBadRequest)
		return
	}

	// 限流 (与 cookie 版同)
	for _, key := range []string{"ip:" + ip, "user:" + req.Username} {
		locked, retryAfter := h.limiter.Check(key)
		if locked {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
			writeJSONError(w, "too many failed attempts, locked", http.StatusTooManyRequests)
			return
		}
	}

	token, user, err := h.manager.Login(r.Context(), req.Username, req.Password, ip, ua)
	if err != nil {
		for _, key := range []string{"ip:" + ip, "user:" + req.Username} {
			_, _ = h.limiter.RecordFailure(key)
		}
		writeJSONError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	// 成功 → 清限流
	for _, key := range []string{"ip:" + ip, "user:" + req.Username} {
		h.limiter.RecordSuccess(key)
	}

	// 取 session 真实 TTL
	sess, err := h.manager.GetSession(r.Context(), token)
	if err != nil || sess == nil {
		writeJSONError(w, "internal: session not found", http.StatusInternalServerError)
		return
	}
	ttlSeconds := int(time.Until(sess.ExpiresAt).Seconds())
	if ttlSeconds <= 0 {
		ttlSeconds = 24 * 3600 // 兜底 24h
	}

	writeJSONOK(w, LoginJWTResponse{
		AccessToken: token,
		ExpiresIn:   ttlSeconds,
		TokenType:   "Bearer",
		Username:    user.Username,
		Role:        user.Role,
	})
}

// extractToken 从 Request 提取 token (Cookie 或 Bearer header).
//
// 优先级:
//  1. Authorization: Bearer <token>  (Phase 2 桌面 app 用)
//  2. Cookie <name>=<token>           (Phase 1 web 端, 向后兼容)
//
// 返回 token (找不到返空字符串).
func extractToken(r *http.Request, cookieName string) string {
	// 1. Bearer header
	if h := r.Header.Get("Authorization"); h != "" {
		const p = "Bearer "
		if strings.HasPrefix(h, p) {
			tok := strings.TrimSpace(h[len(p):])
			if tok != "" {
				return tok
			}
		}
	}
	// 2. Cookie
	if c, err := r.Cookie(cookieName); err == nil {
		tok := strings.TrimSpace(c.Value)
		if tok != "" {
			return tok
		}
	}
	return ""
}

// meJWTHandler GET /api/auth/me (Phase 2 桌面 app Bearer 版).
//
// 自动从 Cookie 或 Authorization Bearer 解析 token.
// 现有 cookie 版不动; 这个 handler 也接 cookie (向后兼容).
func (h *AuthHandler) meJWT(w http.ResponseWriter, r *http.Request) {
	tok := extractToken(r, "novel2all_session")
	user, err := h.manager.GetUserByToken(r.Context(), tok)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSONOK(w, MeResponse{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
	})
}

// 辅助函数 (避免改其他文件).
func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSONOK(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

// Compile-time 防止 unused import 报错.
var _ = strings.HasPrefix
