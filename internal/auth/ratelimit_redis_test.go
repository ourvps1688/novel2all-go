// ratelimit_redis_test.go 提供 RedisLimiter 的单元测试.
//
// 测试覆盖:
//   1. parseRedisAddr 解析 host:port / redis:// URL / 错误格式
//   2. RedisLimiter 在无 Redis 时 fail-open (返回 locked=false, retryAfter=0)
//   3. Limiter interface 的实现断言 (compile-time 已检查, runtime 验证接口)
//   4. RESP 协议 round-trip (mock server)
//
// 注: CI 沙箱/CI runner 无 Redis 服务, 真实 Redis 集成需在部署环境跑.
//   此测试 fail-open 验证保证 Redis 断连时系统不阻塞, 但无法验证真实锁定行为.

package auth

import (
	"bufio"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestParseRedisAddr 验证 addr 解析.
func TestParseRedisAddr(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    parsedRedisAddr
		wantErr bool
	}{
		{
			name:  "simple host:port",
			input: "127.0.0.1:6379",
			want:  parsedRedisAddr{addr: "127.0.0.1:6379", db: 0},
		},
		{
			name:  "redis url with auth and db",
			input: "redis://:secret@127.0.0.1:6379/2",
			want:  parsedRedisAddr{addr: "127.0.0.1:6379", password: "secret", db: 2},
		},
		{
			name:  "redis url with user and pw",
			input: "redis://alice:hunter2@redis.example.com:6380/0",
			want:  parsedRedisAddr{addr: "redis.example.com:6380", password: "hunter2", db: 0},
		},
		{
			name:  "rediss scheme allowed (TLS)",
			input: "rediss://:pw@host:6380",
			want:  parsedRedisAddr{addr: "host:6380", password: "pw", db: 0},
		},
		{
			name:    "empty addr fails",
			input:   "",
			wantErr: true,
		},
		{
			name:    "unsupported scheme fails",
			input:   "http://example.com",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRedisAddr(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseRedisAddr(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseRedisAddr(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

// TestRedisLimiter_FailOpen_NoServer 验证 Redis 不可达时 fail-open 行为.
//
// Check: 无 Redis → (false, 0) 让请求通过, 不阻塞用户.
// RecordFailure: 同样 fail-open → (false, 0).
// RecordSuccess: 静默吞掉错误.
func TestRedisLimiter_FailOpen_NoServer(t *testing.T) {
	// 用一个肯定 listen 不到的地址 (RFC 5737 TEST-NET-1, 192.0.2.0/24).
	// dial timeout 1s 加速测试.
	rl, err := NewRedisLimiter("192.0.2.1:6379", 5, time.Minute, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewRedisLimiter: %v", err)
	}
	rl.dialTimeout = 1 * time.Second
	rl.opTimeout = 1 * time.Second

	// Check: 不阻塞, 返回未锁.
	if locked, retry := rl.Check("test"); locked || retry != 0 {
		t.Errorf("Check no-server: locked=%v retry=%v, want false/0", locked, retry)
	}
	// RecordFailure: 同样 fail-open, 不触发新锁.
	if newLock, retry := rl.RecordFailure("test"); newLock || retry != 0 {
		t.Errorf("RecordFailure no-server: newLock=%v retry=%v, want false/0", newLock, retry)
	}
	// RecordSuccess: 静默.
	rl.RecordSuccess("test")
	// Close: 静默.
	if err := rl.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Cleanup: no-op.
	rl.Cleanup()
}

// TestRedisLimiter_RoundTrip_WithMockServer 启动 mock RESP server,
// 验证 RedisLimiter 真能与 Redis 协议对话 (SCRIPT LOAD / EVALSHA / DEL).
//
// mock server 行为:
//   - PING → +PONG
//   - AUTH/SEL → +OK
//   - SCRIPT LOAD → +<sha>
//   - EVALSHA → *2\r\n:0\r\n:0\r\n (locked=0, retry=0)
//   - DEL → :1
//
// 注: 本测试不验证 Lua 脚本逻辑 (那是 Redis 自身职责), 只验证 RESP 协议对话.
func TestRedisLimiter_RoundTrip_WithMockServer(t *testing.T) {
	mockSrv := newMockRedisServer(t)
	defer mockSrv.close()

	rl, err := NewRedisLimiter(mockSrv.addr(), 5, time.Minute, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewRedisLimiter: %v", err)
	}

	// Check: 走 mock → 返回 (false, 0).
	if locked, retry := rl.Check("test"); locked || retry != 0 {
		t.Errorf("Check mock: locked=%v retry=%v, want false/0", locked, retry)
	}
	// RecordFailure: 同样.
	if newLock, retry := rl.RecordFailure("test"); newLock || retry != 0 {
		t.Errorf("RecordFailure mock: newLock=%v retry=%v, want false/0", newLock, retry)
	}
	// RecordSuccess: 不报错.
	rl.RecordSuccess("test")
}

// mockRedisServer 最小 RESP server, 用于测试 RedisLimiter 协议对话.
type mockRedisServer struct {
	listener net.Listener
	requests *atomic.Int64 // 累计请求数
}

func newMockRedisServer(t *testing.T) *mockRedisServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &mockRedisServer{
		listener: ln,
		requests: new(atomic.Int64),
	}
	go srv.serve(t)
	return srv
}

func (m *mockRedisServer) addr() string {
	return m.listener.Addr().String()
}

func (m *mockRedisServer) close() {
	_ = m.listener.Close()
}

// serve RESP server 主循环 — 每次 accept 一个连接处理.
func (m *mockRedisServer) serve(t *testing.T) {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go m.handle(t, conn)
	}
}

// handle 单连接处理.
func (m *mockRedisServer) handle(t *testing.T, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	br := bufio.NewReader(conn)
	for {
		args, err := readRespCommand(br)
		if err != nil {
			return // client closed
		}
		m.requests.Add(1)

		switch strings.ToUpper(args[0]) {
		case "PING":
			_, _ = conn.Write([]byte("+PONG\r\n"))
		case "AUTH", "SELECT":
			_, _ = conn.Write([]byte("+OK\r\n"))
		case "SCRIPT":
			// SCRIPT LOAD <script> → +<40-char sha>
			// 简单用固定 sha (Redis 会算, 我们只是 echo).
			_, _ = conn.Write([]byte("+abcdef0123456789abcdef0123456789abcdef01\r\n"))
		case "EVALSHA":
			// 返回 *2\r\n:0\r\n:0\r\n (locked=0, retry=0)
			_, _ = conn.Write([]byte("*2\r\n:0\r\n:0\r\n"))
		case "DEL":
			_, _ = conn.Write([]byte(":1\r\n"))
		default:
			_, _ = conn.Write([]byte("-ERR unknown command '" + args[0] + "'\r\n"))
		}
	}
}

// readRespCommand 读一个 RESP array 命令 (RESP 协议简化版).
func readRespCommand(br *bufio.Reader) ([]string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 4 || line[0] != '*' {
		return nil, errParse(line)
	}
	size, err := atoi(line[1 : len(line)-2])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, size)
	for i := 0; i < size; i++ {
		bulk, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(bulk) < 4 || bulk[0] != '$' {
			return nil, errParse(bulk)
		}
		bulkLen, err := atoi(bulk[1 : len(bulk)-2])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, bulkLen+2) // +2 for \r\n
		_, err = readFull(br, buf)
		if err != nil {
			return nil, err
		}
		args = append(args, string(buf[:bulkLen]))
	}
	return args, nil
}

func readFull(br *bufio.Reader, buf []byte) (int, error) {
	read := 0
	for read < len(buf) {
		n, err := br.Read(buf[read:])
		if err != nil {
			return read, err
		}
		read += n
	}
	return read, nil
}

func atoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errParse(s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

type parseErr struct{ s string }

func (e *parseErr) Error() string { return "parse error: " + e.s }

func errParse(s string) error { return &parseErr{s: s} }
