package store

import (
	"context"
	"fmt"
	"time"
)

// AuditEvent 审计事件类型
type AuditEvent string

const (
	AuditLogin          AuditEvent = "login"
	AuditLoginFail      AuditEvent = "login_fail"
	AuditLogout         AuditEvent = "logout"
	AuditCreateUser     AuditEvent = "create_user"
	AuditDeleteUser     AuditEvent = "delete_user"
	AuditDisableUser    AuditEvent = "disable_user"
	AuditEnableUser     AuditEvent = "enable_user"
	AuditSessionExpired AuditEvent = "session_expired"
	AuditRateLimited    AuditEvent = "rate_limited"
)

// AuditEntry 审计日志条目
type AuditEntry struct {
	ID        int64
	EventType AuditEvent
	UserID    *int64 // nullable
	Username  string
	IP        string
	UserAgent string
	Success   bool
	Detail    string
	CreatedAt time.Time
}

// WriteAudit 写一条审计日志
// user_id 可为 nil（登录失败时用户不存在）
func (db *DB) WriteAudit(ctx context.Context, e AuditEntry) error {
	var userID any
	if e.UserID != nil {
		userID = *e.UserID
	}
	success := 0
	if e.Success {
		success = 1
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO audit_log (event_type, user_id, username, ip, user_agent, success, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, string(e.EventType), userID, e.Username, e.IP, e.UserAgent, success, e.Detail)
	if err != nil {
		return fmt.Errorf("write audit: %w", err)
	}
	return nil
}

// ListAudit 查审计日志（按时间倒序，分页）
func (db *DB) ListAudit(ctx context.Context, limit, offset int) ([]*AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, event_type, user_id, username, ip, user_agent, success, detail, created_at
		FROM audit_log
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query audit: %w", err)
	}
	defer rows.Close()

	var out []*AuditEntry
	for rows.Next() {
		var e AuditEntry
		var userID *int64
		var eventType string
		var success int
		var createdAt string
		if err := rows.Scan(&e.ID, &eventType, &userID, &e.Username, &e.IP, &e.UserAgent, &success, &e.Detail, &createdAt); err != nil {
			return nil, err
		}
		e.EventType = AuditEvent(eventType)
		e.UserID = userID
		e.Success = success != 0
		parsed, _ := time.Parse("2006-01-02 15:04:05", createdAt)
		if parsed.IsZero() {
			parsed = time.Now().UTC()
		}
		e.CreatedAt = parsed
		out = append(out, &e)
	}
	return out, rows.Err()
}
