package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// Session 配置
type SessionConfig struct {
	// CookieName 默认 "novel2all_session"
	CookieName string
	// TTL 默认 7 天
	TTL time.Duration
	// Secure HTTPS only
	Secure bool
	// SameSite 默认 Lax
	SameSite http.SameSite
}

// DefaultSessionConfig 返回默认配置
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		CookieName: "novel2all_session",
		TTL:        7 * 24 * time.Hour,
		Secure:     false, // P1 阶段默认 false（HTTP 部署），生产 HTTPS 改 true
		SameSite:   http.SameSiteLaxMode,
	}
}

// ErrUnauthorized 未认证
var ErrUnauthorized = errors.New("unauthorized")

// SessionManager 负责 session 的创建/验证/删除 + cookie 读写
type SessionManager struct {
	store  *store.DB
	config SessionConfig
}

// NewSessionManager 创建
func NewSessionManager(s *store.DB, config SessionConfig) *SessionManager {
	if config.CookieName == "" {
		config.CookieName = "novel2all_session"
	}
	if config.TTL == 0 {
		config.TTL = 7 * 24 * time.Hour
	}
	return &SessionManager{store: s, config: config}
}

// RegisterInput 注册用户输入
type RegisterInput struct {
	Username string
	Password string
	Role     string // 默认 "user"
}

// Register 创建新用户（admin 可指定 role，普通用户强制 user）
func (m *SessionManager) Register(ctx context.Context, input RegisterInput) (*store.User, error) {
	if input.Username == "" || input.Password == "" {
		return nil, fmt.Errorf("username and password required")
	}
	if len(input.Password) < 6 {
		return nil, fmt.Errorf("password too short (min 6 chars)")
	}
	if len(input.Username) < 3 {
		return nil, fmt.Errorf("username too short (min 3 chars)")
	}

	role := input.Role
	if role == "" {
		role = "user"
	}
	if role != "admin" && role != "user" {
		return nil, fmt.Errorf("invalid role: %q", role)
	}

	hash, err := HashPassword(input.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	id, err := m.store.CreateUser(ctx, input.Username, hash, role)
	if err != nil {
		return nil, err
	}

	return m.store.GetUserByID(ctx, id)
}

// ListUsers 列出所有用户（admin guard）
func (m *SessionManager) ListUsers(ctx context.Context) ([]*store.User, error) {
	return m.store.ListUsers(ctx)
}

// GetUserByID 直接按 ID 查用户（不验 session）
func (m *SessionManager) GetUserByID(ctx context.Context, id int64) (*store.User, error) {
	return m.store.GetUserByID(ctx, id)
}

// ListAudit 查审计日志（admin guard 用）
func (m *SessionManager) ListAudit(ctx context.Context, limit, offset int) ([]*store.AuditEntry, error) {
	return m.store.ListAudit(ctx, limit, offset)
}

// DeleteUser 删除用户（admin guard，self-delete 防护）
func (m *SessionManager) DeleteUser(ctx context.Context, requestor, target *store.User) error {
	if !IsAdmin(requestor) {
		return fmt.Errorf("admin required")
	}
	if requestor.ID == target.ID {
		return fmt.Errorf("cannot delete yourself")
	}
	return m.store.DeleteUser(ctx, target.ID)
}

// Login 验证用户名密码，成功返回新 session token
func (m *SessionManager) Login(ctx context.Context, username, password, ip, userAgent string) (string, *store.User, error) {
	u, err := m.store.GetUserByUsername(ctx, username)
	if err != nil {
		// 用户不存在也返回通用错误（不泄露用户存在性）
		_ = m.store.WriteAudit(ctx, store.AuditEntry{
			EventType: store.AuditLoginFail,
			Username:  username,
			IP:        ip,
			UserAgent: userAgent,
			Success:   false,
			Detail:    "user not found",
		})
		return "", nil, fmt.Errorf("%w: invalid credentials", ErrUnauthorized)
	}

	if u.Disabled {
		_ = m.store.WriteAudit(ctx, store.AuditEntry{
			EventType: store.AuditLoginFail,
			UserID:    &u.ID,
			Username:  u.Username,
			IP:        ip,
			UserAgent: userAgent,
			Success:   false,
			Detail:    "user disabled",
		})
		return "", nil, fmt.Errorf("%w: account disabled", ErrUnauthorized)
	}

	// 验证密码
	if err := VerifyPassword(u.PasswordHash, password); err != nil {
		_ = m.store.WriteAudit(ctx, store.AuditEntry{
			EventType: store.AuditLoginFail,
			UserID:    &u.ID,
			Username:  u.Username,
			IP:        ip,
			UserAgent: userAgent,
			Success:   false,
			Detail:    "password mismatch",
		})
		return "", nil, fmt.Errorf("%w: invalid credentials", ErrUnauthorized)
	}

	// 创建 session
	token, err := m.store.CreateSession(ctx, u.ID, ip, userAgent, m.config.TTL)
	if err != nil {
		return "", nil, fmt.Errorf("create session: %w", err)
	}

	// 写成功审计
	uid := u.ID
	_ = m.store.WriteAudit(ctx, store.AuditEntry{
		EventType: store.AuditLogin,
		UserID:    &uid,
		Username:  u.Username,
		IP:        ip,
		UserAgent: userAgent,
		Success:   true,
	})

	return token, u, nil
}

// Logout 删 session（无 token 也成功，幂等）
func (m *SessionManager) Logout(ctx context.Context, token, ip, userAgent string) error {
	if token == "" {
		return nil
	}
	// 先查 user_id 给 audit
	s, err := m.store.GetSession(ctx, token)
	if err == nil {
		uid := s.UserID
		_ = m.store.WriteAudit(ctx, store.AuditEntry{
			EventType: store.AuditLogout,
			UserID:    &uid,
			Username:  "",
			IP:        ip,
			UserAgent: userAgent,
			Success:   true,
		})
	}
	return m.store.DeleteSession(ctx, token)
}

// GetUserByToken 验证 token + 返回用户（中间件用）
func (m *SessionManager) GetUserByToken(ctx context.Context, token string) (*store.User, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	s, err := m.store.GetSession(ctx, token)
	if err != nil {
		return nil, ErrUnauthorized
	}
	u, err := m.store.GetUserByID(ctx, s.UserID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if u.Disabled {
		return nil, ErrUnauthorized
	}
	return u, nil
}

// SetCookie 把 token 写到 cookie
func (m *SessionManager) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.config.CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(m.config.TTL.Seconds()),
		HttpOnly: true,
		Secure:   m.config.Secure,
		SameSite: m.config.SameSite,
	})
}

// ClearCookie 清空 cookie
func (m *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.config.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.config.Secure,
		SameSite: m.config.SameSite,
	})
}

// GetTokenFromRequest 从 cookie 读 token
func (m *SessionManager) GetTokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(m.config.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
