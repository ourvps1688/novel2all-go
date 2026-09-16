package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/auth"
)

// AuthHandler 暴露 /api/auth/* 路由
type AuthHandler struct {
	manager *auth.SessionManager
	limiter *auth.RateLimiter
}

// NewAuthHandler 创建
func NewAuthHandler(m *auth.SessionManager, l *auth.RateLimiter) *AuthHandler {
	return &AuthHandler{manager: m, limiter: l}
}

// LoginRequest POST /api/auth/login
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse POST /api/auth/login
type LoginResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// ServeHTTP 路由分发
//
//	POST /api/auth/login
//	POST /api/auth/logout
//	GET  /api/auth/me
func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/auth")
	path = strings.Trim(path, "/")

	switch path {
	case "login":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.login(w, r)
	case "logout":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.logout(w, r)
	case "me":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.me(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	ua := r.UserAgent()

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %v"}`, err), http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"username and password required"}`, http.StatusBadRequest)
		return
	}

	// 限流检查（按 IP + username 双重 key）
	for _, key := range []string{"ip:" + ip, "user:" + req.Username} {
		locked, retryAfter := h.limiter.Check(key)
		if locked {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
			http.Error(w, `{"error":"too many failed attempts, locked"}`, http.StatusTooManyRequests)
			return
		}
	}

	token, user, err := h.manager.Login(r.Context(), req.Username, req.Password, ip, ua)
	if err != nil {
		// 失败 → 记录
		for _, key := range []string{"ip:" + ip, "user:" + req.Username} {
			newLock, retryAfter := h.limiter.RecordFailure(key)
			if newLock {
				_ = h.manager.Logout // noop to avoid unused
				_ = retryAfter
			}
		}
		_ = h.manager // for clarity
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// 成功 → 清限流计数
	for _, key := range []string{"ip:" + ip, "user:" + req.Username} {
		h.limiter.RecordSuccess(key)
	}

	h.manager.SetCookie(w, token)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(LoginResponse{
		Username: user.Username,
		Role:     user.Role,
	})
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	token := h.manager.GetTokenFromRequest(r)
	ip := clientIP(r)
	ua := r.UserAgent()
	_ = h.manager.Logout(r.Context(), token, ip, ua)
	h.manager.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) me(w http.ResponseWriter, r *http.Request) {
	token := h.manager.GetTokenFromRequest(r)
	user, err := h.manager.GetUserByToken(r.Context(), token)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         user.ID,
		"username":   user.Username,
		"role":       user.Role,
		"created_at": user.CreatedAt,
	})
}

// clientIP 提取客户端 IP（从 X-Forwarded-For 头或 RemoteAddr）
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// 取第一个 IP
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// RemoteAddr 格式 "IP:port"
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		return addr[:idx]
	}
	return addr
}
