package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// newTestAuditHandler 创建临时 DB + 写入测试 audit entries (Sprint V1.0.1 P3: 移除 session 参数).
//
// 鉴权已移到 mux-level middleware. 直接 handler 调用需用 auditReqAdmin helper 注入 admin user context.
func newTestAuditHandler(t *testing.T) (*AuditHandler, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(context.Background(), dir+"/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 先创建用户（audit_log.user_id 有外键约束）
	if _, err := db.CreateUser(context.Background(), "alice", "hash", "user"); err != nil {
		t.Fatalf("create user alice: %v", err)
	}
	if _, err := db.CreateUser(context.Background(), "carol", "hash", "user"); err != nil {
		t.Fatalf("create user carol: %v", err)
	}

	// 写 5 条测试 audit
	uid1 := int64(1)
	uid2 := int64(2)
	entries := []store.AuditEntry{
		{EventType: store.AuditLogin, UserID: &uid1, Username: "alice", IP: "1.1.1.1", Success: true, Detail: "test login 1"},
		{EventType: store.AuditLoginFail, Username: "bob", IP: "2.2.2.2", Success: false, Detail: "wrong password"},
		{EventType: store.AuditLogout, UserID: &uid1, Username: "alice", IP: "1.1.1.1", Success: true, Detail: "logout"},
		{EventType: store.AuditCreateUser, UserID: &uid2, Username: "carol", IP: "3.3.3.3", Success: true, Detail: "created carol"},
		{EventType: store.AuditLoginFail, Username: "eve", IP: "4.4.4.4", Success: false, Detail: "brute force attempt"},
	}
	for i := range entries {
		if err := db.WriteAudit(context.Background(), entries[i]); err != nil {
			t.Fatalf("write audit %d: %v", i, err)
		}
	}

	return NewAuditHandler(db), db
}

// auditReqAdmin 构造带 admin user context 的 request (用于测试 admin-only handler).
func auditReqAdmin(method, url string, body []byte) *http.Request {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, url, bodyReader)
	admin := &store.User{ID: 999, Role: "admin", Username: "test-admin"}
	ctx := context.WithValue(req.Context(), userCtxValue, admin)
	return req.WithContext(ctx)
}

func TestAudit_List_OK(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := auditReqAdmin(http.MethodGet, "/api/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp AuditAdminListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(resp.Entries))
	}
	if resp.Limit != 100 {
		t.Errorf("expected limit=100, got %d", resp.Limit)
	}
}

func TestAudit_List_FilterByEvent(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := auditReqAdmin(http.MethodGet, "/api/audit?event=login_fail", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp AuditAdminListResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	// 应该只有 2 条 login_fail
	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 login_fail entries, got %d", len(resp.Entries))
	}
	for _, e := range resp.Entries {
		if e.EventType != "login_fail" {
			t.Errorf("filter leaked event %q", e.EventType)
		}
	}
}

func TestAudit_List_FilterBySuccess(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := auditReqAdmin(http.MethodGet, "/api/audit?success=false", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp AuditAdminListResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	// 应该有 2 条 success=false (login_fail 都是 false)
	for _, e := range resp.Entries {
		if e.Success {
			t.Errorf("filter leaked success entry: %+v", e)
		}
	}
}

func TestAudit_List_LimitAndOffset(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	// limit=2 offset=0
	req := auditReqAdmin(http.MethodGet, "/api/audit?limit=2&offset=0", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var resp AuditAdminListResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(resp.Entries))
	}
	firstID := resp.Entries[0].ID

	// limit=2 offset=2
	req = auditReqAdmin(http.MethodGet, "/api/audit?limit=2&offset=2", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(resp.Entries))
	}
	if resp.Entries[0].ID >= firstID {
		t.Errorf("offset didn't advance: first.ID=%d, second.ID=%d", firstID, resp.Entries[0].ID)
	}
}

func TestAudit_Get_Success(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	// 取最新一条（最大 ID）
	listReq := auditReqAdmin(http.MethodGet, "/api/audit?limit=1", nil)
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, listReq)
	var listResp AuditAdminListResponse
	_ = json.NewDecoder(listRR.Body).Decode(&listResp)
	targetID := listResp.Entries[0].ID

	// GET /api/audit/{id}
	req := auditReqAdmin(http.MethodGet, "/api/audit/"+intToStr(targetID), nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var entry AuditAdminEntryResponse
	if err := json.NewDecoder(rr.Body).Decode(&entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entry.ID != targetID {
		t.Errorf("expected ID=%d, got %d", targetID, entry.ID)
	}
}

func TestAudit_Get_NotFound(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := auditReqAdmin(http.MethodGet, "/api/audit/999999", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent id, got %d", rr.Code)
	}
}

func TestAudit_Get_InvalidID(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := auditReqAdmin(http.MethodGet, "/api/audit/abc", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid id, got %d", rr.Code)
	}
}

func TestAudit_MethodNotAllowed(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := auditReqAdmin(http.MethodPost, "/api/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rr.Code)
	}
}

// intToStr int64 → string helper（避免 strconv 在测试里多处用）
func intToStr(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(digits[n%10]) + out
		n /= 10
	}
	return out
}

// 防止 unused 警告
var _ = time.Now
