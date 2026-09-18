// Package testfixtures 提供 novel2all-go 测试共享 fixture.
//
// auth.go 实现 Sprint V1.0.1 P0-A 配套: LoginAs + AuthedRequest 测试 helper.
//
// 设计目的:
//   - Sprint V1.0.1 给受保护 endpoint wrap RequireAuth (api.RequireAuth) 后,
//     现有直接调用 handler 的单元测试不受影响 (未走 mux), 但新增的 mux-level
//     集成测试需要 cookie 才能通过.
//   - LoginAs 在 test mux 上注册 + 登录 test user, 返回 cookie 供其他测试复用.
//   - AuthedRequest 把 cookie 附到 http.Request 后通过 mux 发送, 返回 *http.Response.
//
// 使用方式:
//
//	func TestSomething(t *testing.T) {
//	    mux := setupFullMux(t)  // mux 含 /api/auth/* + 受保护 /api/projects/*
//	    cookie := testfixtures.LoginAs(t, mux, "alice", "alicepass")
//
//	    resp := testfixtures.AuthedRequest(t, mux, http.MethodGet, "/api/projects", nil, cookie)
//	    defer resp.Body.Close()
//
//	    if resp.StatusCode != http.StatusOK { ... }
//	}
//
// 不依赖 api 包 — 直接用 httptest 发送 JSON 请求, 避免循环引用.
package testfixtures

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// LoginAs 注册并登录 test user, 返回 session cookie.
//
// 流程:
//  1. POST /api/auth/register {username, password} — 忽略 409 (user 已存在), 其他错误 fail
//  2. POST /api/auth/login {username, password} — 必须 200, 否则 fail
//  3. 从 login 响应 Set-Cookie 头取第一个 cookie (即 session cookie)
//
// mux 必须已注册 /api/auth/register + /api/auth/login 路由 (例如通过 setupUsersTestMux 模式).
//
// 失败时调用 t.Fatal — 测试直接终止.
//
// 注意: 不会重复 lock 测试 IP (register/login 都用同一 IP, 限流计数会累加).
// 如需避免限流, 每个测试用唯一 username.
func LoginAs(t *testing.T, mux http.Handler, username, password string) *http.Cookie {
	t.Helper()

	// 1. 注册 (best-effort, 409 已存在不算错)
	regBody, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	regReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	mux.ServeHTTP(regRec, regReq)

	// 201 Created = 成功, 409 Conflict = 已存在 (idempotent OK), 其他 = 真错
	if regRec.Code != http.StatusCreated && regRec.Code != http.StatusConflict {
		t.Fatalf("LoginAs: register 失败: status=%d body=%s", regRec.Code, regRec.Body.String())
	}

	// 2. 登录
	loginBody, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	mux.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("LoginAs: login 失败: status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}

	// 3. 取 cookie
	resp := loginRec.Result()
	defer func() { _ = resp.Body.Close() }()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("LoginAs: login 响应未设置 session cookie")
	}

	return cookies[0]
}

// AuthedRequest 发送带 session cookie 的 HTTP 请求, 返回 *http.Response.
//
// 调用方负责 resp.Body.Close().
//
// body 非 nil 时:
//   - 用作 request body
//   - Content-Type 自动设为 application/json
//
// cookie 非 nil 时:
//   - 自动 AddCookie 到 request
//
// 测试 fail 条件: 仅在发送请求本身 panic 时 (实际不会发生, 除非 mux 内部 panic).
func AuthedRequest(t *testing.T, mux http.Handler, method, url string, body []byte, cookie *http.Cookie) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req := httptest.NewRequest(method, url, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Result()
}

// AuthedRequestRecorder 同 AuthedRequest 但返回 *httptest.ResponseRecorder (保留 body 缓冲).
//
// 用于需要直接读 Body.String() 的测试 (避免 httptest.ResponseRecorder.Result() 隐式 consume).
func AuthedRequestRecorder(t *testing.T, mux http.Handler, method, url string, body []byte, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req := httptest.NewRequest(method, url, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
