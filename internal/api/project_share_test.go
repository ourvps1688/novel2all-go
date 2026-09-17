package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/store"
)

// shareTestCtx 测试 fixture（合并 6 个返回值为 struct，避免 tooManyResults）
type shareTestCtx struct {
	h        *ProjectShareHandler
	db       *store.DB
	adminTok string
	userTok  string
	adminID  int64
	userID   int64
}

// setupShareTest 创建测试 fixtures：admin + user + 各自的 session
func setupShareTest(t *testing.T) *shareTestCtx {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	fakeHash := "fake_hash_for_test_purposes_only"
	adminID, errCreate := db.CreateUser(ctx, "admin_share", fakeHash, "admin")
	if errCreate != nil {
		t.Fatalf("create admin: %v", errCreate)
	}
	userID, errCreate2 := db.CreateUser(ctx, "user_share", fakeHash, "user")
	if errCreate2 != nil {
		t.Fatalf("create user: %v", errCreate2)
	}

	// 直接 INSERT sessions 表
	adminTok := "admin_token_test"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, ip, user_agent)
		VALUES (?, ?, datetime('now', '+1 day'), '127.0.0.1', 'test')
	`, adminTok, adminID); err != nil {
		t.Fatalf("insert admin session: %v", err)
	}
	userTok := "user_token_test"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, ip, user_agent)
		VALUES (?, ?, datetime('now', '+1 day'), '127.0.0.1', 'test')
	`, userTok, userID); err != nil {
		t.Fatalf("insert user session: %v", err)
	}

	sm := auth.NewSessionManager(db, auth.DefaultSessionConfig())
	return &shareTestCtx{
		h:        NewProjectShareHandler(sm, db),
		db:       db,
		adminTok: adminTok,
		userTok:  userTok,
		adminID:  adminID,
		userID:   userID,
	}
}

func withCookie(req *http.Request, token string) *http.Request {
	req.AddCookie(&http.Cookie{Name: "novel2all_session", Value: token})
	return req
}

func TestProjectShare_Grant(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	adminToken := ctx.adminTok
	adminID := ctx.adminID
	userID := ctx.userID

	form := url.Values{}
	form.Set("user_id", fmtInt(userID))
	form.Set("role", "editor")

	req := httptest.NewRequest(http.MethodPost,
		"/api/auth/projects/my-novel/share/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookie(req, adminToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var m ProjectMembershipResp
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m.Role != "editor" {
		t.Errorf("role=%q, want editor", m.Role)
	}
	if m.GrantedBy != adminID {
		t.Errorf("granted_by=%d, want %d", m.GrantedBy, adminID)
	}
	_ = userID
}

func fmtInt(n int64) string {
	// 小 helper
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}

func TestProjectShare_Grant_NonAdmin(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	userToken := ctx.userTok
	userID := ctx.userID

	form := url.Values{}
	form.Set("user_id", fmtInt(userID))
	form.Set("role", "editor")

	req := httptest.NewRequest(http.MethodPost,
		"/api/auth/projects/my-novel/share/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookie(req, userToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403", rec.Code)
	}
}

func TestProjectShare_Grant_NoAuth(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	userID := ctx.userID

	form := url.Values{}
	form.Set("user_id", fmtInt(userID))
	req := httptest.NewRequest(http.MethodPost,
		"/api/auth/projects/my-novel/share/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// 不带 cookie
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", rec.Code)
	}
}

func TestProjectShare_Grant_Duplicate(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	adminToken := ctx.adminTok
	userID := ctx.userID

	form := url.Values{}
	form.Set("user_id", fmtInt(userID))
	form.Set("role", "viewer")

	// 第一次
	req := httptest.NewRequest(http.MethodPost,
		"/api/auth/projects/proj-dup/share/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookie(req, adminToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 第二次 - 应返回 409
	req2 := httptest.NewRequest(http.MethodPost,
		"/api/auth/projects/proj-dup/share/", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookie(req2, adminToken)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", rec2.Code)
	}
}

func TestProjectShare_Grant_InvalidRole(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	adminToken := ctx.adminTok
	userID := ctx.userID

	form := url.Values{}
	form.Set("user_id", fmtInt(userID))
	form.Set("role", "super-admin")

	req := httptest.NewRequest(http.MethodPost,
		"/api/auth/projects/proj-role/share/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookie(req, adminToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestProjectShare_ListMembers(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	db := ctx.db
	adminToken := ctx.adminTok
	userID := ctx.userID
	bgCtx := context.Background()

	// 显式 grant（保留前导 /）
	_, err := db.GrantProjectAccess(bgCtx, userID, "/list-test", "editor", 1)
	if err != nil {
		t.Fatalf("grant in test: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/projects/list-test/share/", http.NoBody)
	withCookie(req, adminToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Members []ProjectMembershipResp `json:"members"`
		Count   int                     `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 1 {
		t.Errorf("count=%d, want 1", resp.Count)
	}
}

func TestProjectShare_Revoke(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	db := ctx.db
	adminToken := ctx.adminTok
	userID := ctx.userID
	bgCtx := context.Background()

	_, err := db.GrantProjectAccess(bgCtx, userID, "/revoke-test", "viewer", 1)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/api/auth/projects/revoke-test/share/"+fmtInt(userID)+"/", http.NoBody)
	withCookie(req, adminToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status=%d, want 204", rec.Code)
	}

	// 验证已删除
	_, err = db.GetProjectMembership(bgCtx, userID, "/revoke-test")
	if err == nil {
		t.Error("membership should be deleted")
	}
}

func TestProjectShare_Revoke_NotFound(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	adminToken := ctx.adminTok

	req := httptest.NewRequest(http.MethodDelete,
		"/api/auth/projects/proj/share/9999/", http.NoBody)
	withCookie(req, adminToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestProjectShare_ListUserProjects(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	db := ctx.db
	userToken := ctx.userTok
	userID := ctx.userID
	bgCtx := context.Background()

	_, _ = db.GrantProjectAccess(bgCtx, userID, "/proj-a-list", "editor", 1)
	_, _ = db.GrantProjectAccess(bgCtx, userID, "/proj-b-list", "viewer", 1)

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/users/"+fmtInt(userID)+"/projects/", http.NoBody)
	withCookie(req, userToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Projects []ProjectMembershipResp `json:"projects"`
		Count    int                     `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 2 {
		t.Errorf("count=%d, want 2", resp.Count)
	}
}

func TestProjectShare_UnknownPath(t *testing.T) {
	ctx := setupShareTest(t)
	h := ctx.h
	adminToken := ctx.adminTok
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/something/", http.NoBody)
	withCookie(req, adminToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

func TestCleanProjectPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"./proj", "/proj"},
		{"././proj", "/proj"},
		{"proj/", "/proj"},
		{"path/to/proj", "/path/to/proj"},
		{"  spaces  ", "/spaces"},
		{"/already", "/already"},
		{"", ""},
	}
	for _, c := range cases {
		got := cleanProjectPath(c.in)
		if got != c.want {
			t.Errorf("cleanProjectPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidRole(t *testing.T) {
	for _, r := range []string{"owner", "editor", "viewer"} {
		if !validRole(r) {
			t.Errorf("validRole(%q) = false, want true", r)
		}
	}
	for _, r := range []string{"admin", "super-admin", ""} {
		if validRole(r) {
			t.Errorf("validRole(%q) = true, want false", r)
		}
	}
}

// 防止 unused 警告
var _ = bytes.NewReader
var _ = io.Discard
