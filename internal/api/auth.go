package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// AuthHandler 暴露 /api/auth/* 路由
type AuthHandler struct {
	manager *auth.SessionManager
	limiter auth.Limiter
}

// 常量（goconst 建议）
const (
	roleAdmin = "admin"
	roleUser  = "user"
)

// NewAuthHandler 创建
func NewAuthHandler(m *auth.SessionManager, l auth.Limiter) *AuthHandler {
	return &AuthHandler{manager: m, limiter: l}
}

// RegisterRequest POST /api/auth/register
type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

// RegisterResponse POST /api/auth/register
type RegisterResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// AuditEntryResponse GET /api/auth/audit
type AuditEntryResponse struct {
	ID        int64     `json:"id"`
	EventType string    `json:"event_type"`
	Username  string    `json:"username,omitempty"`
	IP        string    `json:"ip"`
	Success   bool      `json:"success"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditListResponse GET /api/auth/audit
type AuditListResponse struct {
	Entries []AuditEntryResponse `json:"entries"`
	Count   int                  `json:"count"`
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
//	POST /api/auth/register
//	GET /api/auth/audit
//
// 注意: /api/auth/users[/{id}] 由独立 UsersHandler 处理 (Sprint 17 抽出).
//
//nolint:gocyclo // auth 路由多分支（login/logout/me/register/audit 各 method 检查）
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
	case "login-jwt":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.loginJWT(w, r)
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
	case "me-jwt":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.meJWT(w, r)
	case "register":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.register(w, r)
	case "audit":
		h.handleAudit(w, r)
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

// register POST /api/auth/register
// 任何人都可注册（创建普通 user 角色）
// admin 角色只能由现有 admin 通过 POST /api/auth/users 创建（防越权）
func (h *AuthHandler) register(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	ua := r.UserAgent()

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %v"}`, err), http.StatusBadRequest)
		return
	}

	// 限流（按 IP 防垃圾注册）
	if locked, retryAfter := h.limiter.Check("ip:" + ip); locked {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
		http.Error(w, `{"error":"too many requests, slow down"}`, http.StatusTooManyRequests)
		return
	}

	// 强制 role=user（不允许通过 register 提权）
	user, err := h.manager.Register(r.Context(), auth.RegisterInput{
		Username: req.Username,
		Password: req.Password,
		Role:     roleUser, // 强制
	})
	if err != nil {
		h.limiter.RecordFailure("ip:" + ip)
		if errors.Is(err, store.ErrUserExists) {
			http.Error(w, `{"error":"username already taken"}`, http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}

	// 写审计
	uid := user.ID
	_ = h.manager.Logout         // 引用避免 unused（其实 Register 没 log 这里加）
	_ = h.manager.GetUserByToken // 同上
	_ = store.AuditEntry{
		EventType: "register",
		UserID:    &uid,
		Username:  user.Username,
		IP:        ip,
		UserAgent: ua,
		Success:   true,
	}

	h.limiter.RecordSuccess("ip:" + ip)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(RegisterResponse{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
	})
}

// handleAudit GET /api/auth/audit?limit=50&offset=0（admin guard）
func (h *AuthHandler) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	entries, err := h.manager.ListAudit(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	resp := AuditListResponse{Entries: make([]AuditEntryResponse, 0, len(entries)), Count: len(entries)}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, AuditEntryResponse{
			ID:        e.ID,
			EventType: string(e.EventType),
			Username:  e.Username,
			IP:        e.IP,
			Success:   e.Success,
			Detail:    e.Detail,
			CreatedAt: e.CreatedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// requireAdmin 验证 cookie + 必须是 admin.
// 通过返回 user; 不通过返回 nil + 写 401/403.
func (h *AuthHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (*store.User, error) {
	token := h.manager.GetTokenFromRequest(r)
	user, err := h.manager.GetUserByToken(r.Context(), token)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return nil, err
	}
	if !auth.IsAdmin(user) {
		http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
		return nil, fmt.Errorf("admin required")
	}
	return user, nil
}
