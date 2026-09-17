package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// newTestAuditHandler 创建临时 DB + 写入测试 audit entries
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

	session := newMockLookup("admin-token", "user-token")
	return NewAuditHandler(db, session), db
}

func TestAudit_List_RequiresAuth(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAudit_List_RequiresAdmin(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "user-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestAudit_List_AdminSuccess(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
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

	req := httptest.NewRequest(http.MethodGet, "/api/audit?event=login_fail", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
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

	req := httptest.NewRequest(http.MethodGet, "/api/audit?success=false", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
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
	req := httptest.NewRequest(http.MethodGet, "/api/audit?limit=2&offset=0", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var resp AuditAdminListResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(resp.Entries))
	}
	firstID := resp.Entries[0].ID

	// limit=2 offset=2
	req = httptest.NewRequest(http.MethodGet, "/api/audit?limit=2&offset=2", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
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
	listReq := httptest.NewRequest(http.MethodGet, "/api/audit?limit=1", nil)
	listReq.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, listReq)
	var listResp AuditAdminListResponse
	_ = json.NewDecoder(listRR.Body).Decode(&listResp)
	targetID := listResp.Entries[0].ID

	// GET /api/audit/{id}
	req := httptest.NewRequest(http.MethodGet, "/api/audit/"+intToStr(targetID), nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
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

	req := httptest.NewRequest(http.MethodGet, "/api/audit/999999", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent id, got %d", rr.Code)
	}
}

func TestAudit_Get_InvalidID(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/audit/abc", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid id, got %d", rr.Code)
	}
}

func TestAudit_MethodNotAllowed(t *testing.T) {
	h, _ := newTestAuditHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/audit", nil)
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: "admin-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rr.Code)
	}
}

func TestAudit_NilSessionReturns503(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(context.Background(), dir+"/test.db")
	defer func() { _ = db.Close() }()
	_ = db.Migrate(context.Background())

	h := NewAuditHandler(db, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with nil session, got %d", rr.Code)
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
