// ratelimit_redis.go 提供 Redis 版限流器实现.
//
// 设计要点:
//   - 用 Lua 脚本原子化 "累加 + 过期 + 判定锁定" 操作, 避免 race
//   - 用 RESP2 协议直连 Redis (自写极简 client), 不依赖 go-redis 等重库
//   - 连接断开时返回 ErrLimiterUnavailable, 上层 fail-open (auth handler 不阻塞)
//   - Cleanup 是 no-op (Redis 自己用 TTL 过期 key)
//
// Lua 脚本逻辑 (key 命名 "novel2all:ratelimit:<key>"):
//
//	count = INCR(key)
//	if count == 1 then EXPIRE(key, window_seconds) end
//	if count >= limit then EXPIRE(key, lockout_seconds); TTL(key); return {1, ttl}
//	return {0, 0}
//
// Sprint V1.0.1 P5: RateLimiter 多进程支持. 单进程可继续用内存版 RateLimiter,
// 设 REDIS_URL 环境变量自动切换到 RedisLimiter.
package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// redisScript Lua 脚本: 原子化 Check+RecordFailure.
//
// KEYS[1] = ratelimit:<key>
// ARGV[1] = limit
// ARGV[2] = window_seconds
// ARGV[3] = lockout_seconds
//
// 返回 {locked_int, retry_after_seconds_int}.
const redisScript = `
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("EXPIRE", KEYS[1], tonumber(ARGV[2]))
end
if count >= tonumber(ARGV[1]) then
  redis.call("EXPIRE", KEYS[1], tonumber(ARGV[3]))
  local ttl = redis.call("TTL", KEYS[1])
  return {1, ttl}
end
return {0, 0}
`

// redisCheckScript Check 操作: 不累加, 只查锁定状态 + 剩余 TTL.
//
// KEYS[1] = ratelimit:<key>
const redisCheckScript = `
local ttl = redis.call("TTL", KEYS[1])
if ttl > 0 then
  return {1, ttl}
end
return {0, 0}
`

// RedisLimiter Redis 版限流器实现.
//
// 线程安全:
//   - connMu 保护 conn (tcp 连接不是 goroutine-safe)
//   - stateMu 保护 lastLocked/lastRetry (parseResult 与 lastResult 共享)
//   - scriptSha atomic.Pointer 缓存 SCRIPT LOAD SHA1
type RedisLimiter struct {
	addr        string
	password    string
	db          int
	limit       int
	window      time.Duration
	lockoutTime time.Duration

	scriptSha      atomic.Pointer[string]
	checkScriptSha atomic.Pointer[string]

	connMu sync.Mutex
	conn   net.Conn
	br     *bufio.Reader
	bw     *bufio.Writer

	stateMu    sync.Mutex
	lastLocked bool
	lastRetry  time.Duration

	dialTimeout time.Duration
	opTimeout   time.Duration

	keyPrefix string
}

// Compile-time 断言: RedisLimiter 实现 Limiter 接口.
var _ Limiter = (*RedisLimiter)(nil)

// NewRedisLimiter 创建 Redis 限流器.
//
// addr: "host:port" 或 "redis://user:pass@host:port/db" (见 parseRedisAddr).
// limit/window/lockoutTime: 与 NewRateLimiter 同义.
//
// 连接是惰性建立 (第一次操作时 dial).
func NewRedisLimiter(addr string, limit int, window, lockoutTime time.Duration) (*RedisLimiter, error) {
	parsed, err := parseRedisAddr(addr)
	if err != nil {
		return nil, err
	}
	return &RedisLimiter{
		addr:        parsed.addr,
		password:    parsed.password,
		db:          parsed.db,
		limit:       limit,
		window:      window,
		lockoutTime: lockoutTime,
		dialTimeout: 5 * time.Second,
		opTimeout:   2 * time.Second,
		keyPrefix:   "novel2all:ratelimit:",
	}, nil
}

// parseRedisAddr 解析 redis:// URL 或 host:port.
type parsedRedisAddr struct {
	addr     string
	password string
	db       int
}

func parseRedisAddr(s string) (parsedRedisAddr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return parsedRedisAddr{}, errors.New("empty redis addr")
	}
	if !strings.Contains(s, "://") {
		return parsedRedisAddr{addr: s, db: 0}, nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return parsedRedisAddr{}, fmt.Errorf("parse redis url: %w", err)
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return parsedRedisAddr{}, fmt.Errorf("unsupported redis scheme: %s (only redis/rediss)", u.Scheme)
	}
	out := parsedRedisAddr{addr: u.Host, db: 0}
	if u.User != nil {
		if pw, ok := u.User.Password(); ok {
			out.password = pw
		}
	}
	if u.Path != "" && u.Path != "/" {
		if db, err := strconv.Atoi(strings.TrimPrefix(u.Path, "/")); err == nil {
			out.db = db
		}
	}
	return out, nil
}

// connect 拨号 + AUTH + SELECT (若配置) + PING + SCRIPT LOAD.
//
// connMu 保护.
func (r *RedisLimiter) connect(ctx context.Context) error {
	if r.conn != nil {
		return nil
	}
	d := net.Dialer{Timeout: r.dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", r.addr)
	if err != nil {
		return fmt.Errorf("redis dial %s: %w", r.addr, err)
	}
	r.conn = conn
	r.br = bufio.NewReader(conn)
	r.bw = bufio.NewWriter(conn)

	if r.password != "" {
		if err := r.simpleCmd(ctx, "AUTH", r.password); err != nil {
			_ = conn.Close()
			r.conn = nil
			return fmt.Errorf("redis auth: %w", err)
		}
	}
	if r.db != 0 {
		if err := r.simpleCmd(ctx, "SELECT", strconv.Itoa(r.db)); err != nil {
			_ = conn.Close()
			r.conn = nil
			return fmt.Errorf("redis select db: %w", err)
		}
	}
	if err := r.simpleCmd(ctx, "PING"); err != nil {
		_ = conn.Close()
		r.conn = nil
		return fmt.Errorf("redis ping: %w", err)
	}

	sha, err := r.scriptLoad(ctx, redisScript)
	if err != nil {
		_ = conn.Close()
		r.conn = nil
		return fmt.Errorf("redis script load (rate-limit): %w", err)
	}
	r.scriptSha.Store(&sha)

	checkSha, err := r.scriptLoad(ctx, redisCheckScript)
	if err != nil {
		_ = conn.Close()
		r.conn = nil
		return fmt.Errorf("redis script load (check): %w", err)
	}
	r.checkScriptSha.Store(&checkSha)

	return nil
}

// disconnect 关闭当前连接 (用于出错重连).
func (r *RedisLimiter) disconnect() {
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
	}
}

// scriptLoad SCRIPT LOAD <script>, 返回 SHA1.
func (r *RedisLimiter) scriptLoad(ctx context.Context, script string) (string, error) {
	if err := r.writeArgs("SCRIPT", "LOAD", script); err != nil {
		return "", err
	}
	reply, err := r.readReply(ctx)
	if err != nil {
		return "", err
	}
	s, ok := reply.(string)
	if !ok || len(s) != 40 {
		return "", fmt.Errorf("script load returned invalid sha1: %v", reply)
	}
	return s, nil
}

// simpleCmd 执行无返回值命令 (AUTH/SELECT/PING).
func (r *RedisLimiter) simpleCmd(ctx context.Context, args ...string) error {
	if err := r.writeArgs(args...); err != nil {
		return err
	}
	reply, err := r.readReply(ctx)
	if err != nil {
		return err
	}
	s, ok := reply.(string)
	if !ok {
		return fmt.Errorf("unexpected reply for %v: %v", args, reply)
	}
	if s == "OK" || s == "PONG" {
		return nil
	}
	return fmt.Errorf("unexpected reply for %v: %s", args, s)
}

// Check 查 key 锁定状态 (不累加).
//
// 失败时返回 (false, 0) (fail-open: 限流器不可用时不阻塞用户).
func (r *RedisLimiter) Check(key string) (bool, time.Duration) {
	if err := r.runScript(key, false); err != nil {
		return false, 0
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.lastLocked, r.lastRetry
}

// RecordFailure 累加并判断锁定.
//
// 失败时返回 (false, 0) (fail-open).
func (r *RedisLimiter) RecordFailure(key string) (newLock bool, retryAfter time.Duration) {
	if err := r.runScript(key, true); err != nil {
		return false, 0
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.lastLocked, r.lastRetry
}

// runScript 执行 Redis 脚本 (recordFailure=true → 累加; false → 只查).
//
// 失败时返回 error (调用方决定 fail-open 或 fail-closed).
func (r *RedisLimiter) runScript(key string, recordFailure bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.opTimeout)
	defer cancel()

	r.connMu.Lock()
	if err := r.connect(ctx); err != nil {
		r.connMu.Unlock()
		return err
	}
	r.connMu.Unlock()

	fullKey := r.keyPrefix + key
	var sha *string
	var args []string
	if recordFailure {
		sha = r.scriptSha.Load()
		if sha == nil {
			return errors.New("rate-limit script not loaded")
		}
		args = []string{
			"EVALSHA", *sha, "1", fullKey,
			strconv.Itoa(r.limit),
			strconv.Itoa(int(r.window.Seconds())),
			strconv.Itoa(int(r.lockoutTime.Seconds())),
		}
	} else {
		sha = r.checkScriptSha.Load()
		if sha == nil {
			return errors.New("check script not loaded")
		}
		args = []string{"EVALSHA", *sha, "1", fullKey}
	}

	r.connMu.Lock()
	defer r.connMu.Unlock()
	if err := r.writeArgs(args...); err != nil {
		r.disconnect()
		return err
	}
	reply, err := r.readReply(ctx)
	if err != nil {
		r.disconnect()
		return err
	}
	return r.parseResult(reply)
}

// parseResult 把 Lua 脚本返回值 [{locked_int}, {retry_after_seconds_int}] 存到 state.
func (r *RedisLimiter) parseResult(reply any) error {
	arr, ok := reply.([]any)
	if !ok || len(arr) != 2 {
		return fmt.Errorf("unexpected reply: %v", reply)
	}
	locked, _ := arr[0].(int64)
	ttl, _ := arr[1].(int64)

	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.lastLocked = locked == 1
	if r.lastLocked && ttl > 0 {
		r.lastRetry = time.Duration(ttl) * time.Second
	} else {
		r.lastRetry = 0
	}
	return nil
}

// RecordSuccess 清空失败计数 (DEL key).
//
// 失败时静默 — best-effort 清理, 不阻塞业务.
func (r *RedisLimiter) RecordSuccess(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), r.opTimeout)
	defer cancel()

	r.connMu.Lock()
	if err := r.connect(ctx); err != nil {
		r.connMu.Unlock()
		return
	}
	r.connMu.Unlock()

	fullKey := r.keyPrefix + key
	r.connMu.Lock()
	defer r.connMu.Unlock()
	if err := r.writeArgs("DEL", fullKey); err != nil {
		r.disconnect()
		return
	}
	_, _ = r.readReply(ctx) // reply 忽略
}

// Cleanup Redis 版 no-op (TTL 自动过期).
func (r *RedisLimiter) Cleanup() {}

// Close 关闭连接 (供 server graceful shutdown 调用).
func (r *RedisLimiter) Close() error {
	r.connMu.Lock()
	defer r.connMu.Unlock()
	if r.conn == nil {
		return nil
	}
	err := r.conn.Close()
	r.conn = nil
	return err
}

// --- RESP protocol helpers ---

// writeArgs RESP array 格式: *<argc>\r\n$<len>\r\n<arg>\r\n...
func (r *RedisLimiter) writeArgs(args ...string) error {
	if _, err := r.bw.WriteString("*" + strconv.Itoa(len(args)) + "\r\n"); err != nil {
		return err
	}
	for _, a := range args {
		if _, err := r.bw.WriteString("$" + strconv.Itoa(len(a)) + "\r\n"); err != nil {
			return err
		}
		if _, err := r.bw.WriteString(a); err != nil {
			return err
		}
		if _, err := r.bw.WriteString("\r\n"); err != nil {
			return err
		}
	}
	return r.bw.Flush()
}

// readReply 读一个 RESP reply with context deadline.
func (r *RedisLimiter) readReply(ctx context.Context) (any, error) {
	type result struct {
		v   any
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := r.readReplyLocked()
		ch <- result{v: v, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.v, r.err
	}
}

// readReplyLocked 阻塞 readReply 实现.
//
//nolint:gocyclo // RESP 协议 switch 天然多分支 (5 种 prefix), 用 helper 拆后 ≤ 7.
func (r *RedisLimiter) readReplyLocked() (any, error) {
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}
	if line == "" {
		return nil, errors.New("empty reply")
	}
	prefix := line[0]
	body := line[1:]
	switch prefix {
	case '+':
		return body, nil
	case '-':
		return nil, errors.New("redis error: " + body)
	case ':':
		return r.parseIntegerReply(body)
	case '$':
		return r.parseBulkReply(body)
	case '*':
		return r.parseArrayReply(body)
	default:
		return nil, fmt.Errorf("unknown reply prefix %q in %q", prefix, line)
	}
}

// parseIntegerReply 解析 :<int>\r\n.
func (r *RedisLimiter) parseIntegerReply(body string) (any, error) {
	n, err := strconv.ParseInt(body, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse int reply %q: %w", body, err)
	}
	return n, nil
}

// parseBulkReply 解析 $<len>\r\n<data>\r\n 或 $-1\r\n (nil).
func (r *RedisLimiter) parseBulkReply(body string) (any, error) {
	size, err := strconv.Atoi(body)
	if err != nil {
		return nil, fmt.Errorf("parse bulk size %q: %w", body, err)
	}
	if size < 0 {
		return nil, nil
	}
	buf := make([]byte, size+2) // +2 for \r\n
	if _, err := io.ReadFull(r.br, buf); err != nil {
		return nil, err
	}
	return string(buf[:size]), nil
}

// parseArrayReply 解析 *<n>\r\n<n 个 reply>...
func (r *RedisLimiter) parseArrayReply(body string) (any, error) {
	size, err := strconv.Atoi(body)
	if err != nil {
		return nil, fmt.Errorf("parse array size %q: %w", body, err)
	}
	if size < 0 {
		return nil, nil
	}
	out := make([]any, size)
	for i := 0; i < size; i++ {
		v, err := r.readReplyLocked()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// readLine 读一行 (\r\n 终止).
func (r *RedisLimiter) readLine() (string, error) {
	line, err := r.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", fmt.Errorf("malformed line: %q", line)
	}
	return line[:len(line)-2], nil
}
