package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestStore 创建一个临时目录 + Store, 自动清理 (t.TempDir).
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_llm_keys.enc")
	// 在测试中绕开 hostname+username 派生, 直接注入 password material.
	// 这样测试不需要真实用户环境, 也可模拟跨机器/跨用户场景.
	s, err := newWithPassword(path, []byte("test-password-material"))
	if err != nil {
		t.Fatalf("newWithPassword: %v", err)
	}
	return s
}

// 测试 round-trip: Set → Get → 验证一致.
func TestSetGet(t *testing.T) {
	s := newTestStore(t)
	cases := []struct {
		provider string
		key      string
	}{
		{"dashscope", "sk-dashscope-test-1234567890abcdef"},
		{"deepseek", "sk-deepseek-test-abcdef1234567890"},
		{"minimax", "eyJhbGciOiJIUzI1NiJ9.test.minimax"},
		{"anthropic", "sk-ant-test-12345"},
	}
	for _, tc := range cases {
		if err := s.Set(tc.provider, tc.key); err != nil {
			t.Fatalf("Set(%s): %v", tc.provider, err)
		}
		got, err := s.Get(tc.provider)
		if err != nil {
			t.Fatalf("Get(%s): %v", tc.provider, err)
		}
		if got != tc.key {
			t.Errorf("Get(%s): got %q, want %q", tc.provider, got, tc.key)
		}
	}
}

// 测试 Has: 区分有 key / 没 key / 不存在的 provider.
func TestHas(t *testing.T) {
	s := newTestStore(t)
	if has, _ := s.Has("dashscope"); has {
		t.Error("Has on empty store: got true, want false")
	}
	if err := s.Set("dashscope", "test-key"); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.Has("dashscope"); !has {
		t.Error("Has after Set: got false, want true")
	}
	if has, _ := s.Has("nonexistent"); has {
		t.Error("Has on missing provider: got true, want false")
	}
}

// 测试 List 返已配置 provider 名称 (sorted, non-empty only).
func TestList(t *testing.T) {
	s := newTestStore(t)
	names, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("List on empty: got %v, want []", names)
	}

	// 按非字母顺序设
	if err := s.Set("minimax", "x"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("dashscope", "y"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("deepseek", "z"); err != nil {
		t.Fatal(err)
	}

	names, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dashscope", "deepseek", "minimax"}
	if !equalStrings(names, want) {
		t.Errorf("List: got %v, want %v", names, want)
	}
}

// 测试 Set("") 等同于删除.
func TestSetEmptyDeletes(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("dashscope", "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("dashscope", ""); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.Has("dashscope"); has {
		t.Error("Has after Set with empty: got true, want false")
	}
}

// 测试 Clear 删除文件.
func TestClear(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("dashscope", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Fatalf("file should exist after Set: %v", err)
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Errorf("file should not exist after Clear: %v", err)
	}
	// Clear on already-empty 也应该 nil
	if err := s.Clear(); err != nil {
		t.Errorf("Clear on empty: %v", err)
	}
}

// 测试篡改密文 → 解密失败 (AES-GCM auth tag 验证).
func TestTamperedCiphertextFails(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("dashscope", "secret"); err != nil {
		t.Fatal(err)
	}
	// 读文件, 改一个 byte
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	// 反序列化, 改 ct 一个 byte, 重写
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	// map element 不可地址 → 临时变量读改写
	dashEntry := f.Providers["dashscope"]
	ct, _ := base64.StdEncoding.DecodeString(dashEntry.CT)
	if len(ct) > 0 {
		ct[0] ^= 0xff // flip byte
	}
	dashEntry.CT = base64.StdEncoding.EncodeToString(ct)
	f.Providers["dashscope"] = dashEntry
	tampered, _ := json.MarshalIndent(f, "", "  ")
	if err := os.WriteFile(s.Path(), tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	// 重新加载: 应该返回 decrypt error
	_, err = s.Get("dashscope")
	if err == nil {
		t.Error("Get on tampered file: got nil error, want decrypt failure")
	}
	if !strings.Contains(err.Error(), "decrypt") {
		t.Errorf("Get on tampered: error should mention decrypt, got: %v", err)
	}
}

// 测试 ciphertext 改名攻击 (AAD 验证).
func TestRenamedProviderFails(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("dashscope", "secret-dashscope"); err != nil {
		t.Fatal(err)
	}
	// 把 dashscope 的 ciphertext 移到 deepseek 名下
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	dashEntry := f.Providers["dashscope"]
	f.Providers["deepseek"] = dashEntry
	tampered, _ := json.MarshalIndent(f, "", "  ")
	if err := os.WriteFile(s.Path(), tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = s.Get("deepseek")
	if err == nil {
		t.Error("Get on renamed ciphertext: got nil, want decrypt failure (AAD mismatch)")
	}
}

// 测试错误 version 拒绝加载.
func TestUnsupportedVersion(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("dashscope", "x"); err != nil {
		t.Fatal(err)
	}
	// 手动改 version
	data, _ := os.ReadFile(s.Path())
	var f storeFile
	_ = json.Unmarshal(data, &f)
	f.Version = 999
	bad, _ := json.MarshalIndent(f, "", "  ")
	if err := os.WriteFile(s.Path(), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get("dashscope")
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("Get on bad version: got %v, want version error", err)
	}
}

// 测试不同 password material 派生不同 key (机器绑定).
func TestDifferentMachineDifferentKey(t *testing.T) {
	dir := t.TempDir()
	s1, _ := newWithPassword(filepath.Join(dir, "a.enc"), []byte("machine-A"))
	s2, _ := newWithPassword(filepath.Join(dir, "b.enc"), []byte("machine-B"))

	if err := s1.Set("dashscope", "secret"); err != nil {
		t.Fatal(err)
	}
	// s2 不应能解 s1 的密文 (即使路径不同, 用 s2 master key 读 s1 文件会失败)
	// 实际场景是 s1/s2 路径一样但 machine 不同; 这里只验证 key 派生确实依赖 password
	data, _ := os.ReadFile(s1.Path())
	var f storeFile
	_ = json.Unmarshal(data, &f)
	// 写到 s2 路径
	if err := os.WriteFile(s2.Path(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s2.Get("dashscope")
	if err == nil {
		t.Error("Get with wrong password: got nil, want decrypt failure")
	}
}

// 测试并发 Set/Get (race detector 验证).
func TestConcurrent(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	providers := []string{"dashscope", "deepseek", "minimax", "anthropic"}
	for _, p := range providers {
		wg.Add(2)
		go func(p string) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = s.Set(p, "key-"+p)
			}
		}(p)
		go func(p string) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = s.Get(p)
			}
		}(p)
	}
	wg.Wait()
	// 验证最终一致性: 至少所有 provider 都有 key
	for _, p := range providers {
		if has, _ := s.Has(p); !has {
			t.Errorf("Has(%s) after concurrent: got false, want true", p)
		}
	}
}

// 测试 DefaultPath 不创建文件.
func TestDefaultPath(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if !strings.HasSuffix(p, "llm_keys.enc") {
		t.Errorf("DefaultPath: got %q, want suffix llm_keys.enc", p)
	}
	// 文件不应该被 DefaultPath 创建
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("DefaultPath should not create file: stat err=%v", err)
	}
}

// 测试 New("") 拒绝空路径.
func TestNewEmptyPath(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Error("New(\"\"): got nil, want error")
	}
}

// 测试 provider 名称空字符串拒绝.
func TestEmptyProviderName(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("", "x"); err == nil {
		t.Error("Set(\"\"): got nil, want error")
	}
	if _, err := s.Get(""); err == nil {
		t.Error("Get(\"\"): got nil, want error")
	}
}

// 测试随机 bytes 不是有效加密文件 (parse 失败).
func TestGarbageFileFails(t *testing.T) {
	s := newTestStore(t)
	// 写随机 bytes
	junk := make([]byte, 100)
	_, _ = rand.Read(junk)
	if err := os.WriteFile(s.Path(), junk, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get("dashscope")
	if err == nil {
		t.Error("Get on garbage file: got nil, want parse error")
	}
}

// --- helpers ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// newWithPassword 创建 Store 但指定 password material (测试用, 绕开 hostname 派生).
func newWithPassword(path string, password []byte) (*Store, error) {
	if path == "" {
		return nil, errors.New("path required")
	}
	return &Store{
		path:     path,
		password: password,
	}, nil
}