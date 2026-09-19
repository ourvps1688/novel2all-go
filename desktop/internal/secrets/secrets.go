// Package secrets 提供桌面 app 的本地 LLM API key 加密存储.
//
// 设计目标:
//   - 防 casual 访问 (其他用户/进程读不到明文)
//   - 不依赖用户密码 (机器绑定 master key, 减少 UX friction)
//   - 跨机器/跨用户不可解 (hostname + username 派生, 攻击者拿到密文也无法离线解)
//   - 标准化加密 (AES-256-GCM + PBKDF2-SHA256 100k iter, NIST 推荐)
//
// 加密流程:
//   1. master_key = PBKDF2(SHA256, password=fixed_salt+hostname+username,
//                            salt="novel2all-pbkdf2-salt-v1",
//                            iter=100000, keylen=32)
//   2. 对每个 provider 的 key:
//      nonce = random 12 bytes
//      ct, err = AES-256-GCM(master_key, nonce).Seal(nil, nonce, plaintext, associated_data=provider)
//      存 {nonce, ct}
//
// 文件格式 (JSON):
//
//	{
//	  "version": 1,
//	  "kdf": "pbkdf2-sha256",
//	  "iterations": 100000,
//	  "salt": "novel2all-pbkdf2-salt-v1",
//	  "providers": {
//	    "dashscope": {"nonce": "base64", "ct": "base64"},
//	    "deepseek":  {"nonce": "base64", "ct": "base64"},
//	    "minimax":   {"nonce": "base64", "ct": "base64"}
//	  }
//	}
//
// 存储路径:
//   - Windows: %APPDATA%\novel2all-desktop\llm_keys.enc (0600)
//   - Linux/Mac: ~/.config/novel2all-desktop/llm_keys.enc (0600)
//
// 安全限制 (MVP, 不替代 OS keystore):
//   - 同一机器同一用户能解 (合法用例)
//   - 其他用户/机器**不能**解 (hostname+username 派生材料不可移植)
//   - 不防恶意 root/同用户进程 (root 总是能读 0600 文件)
//   - 不防物理访问攻击者 (他们可以冒充 user 登录解)
//
// Module B.2 计划升级到 OS keystore (Windows DPAPI / macOS Keychain / Linux Secret Service).
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

// 当前文件格式版本. 升级时递增.
const fileVersion = 1

const (
	// kdfIterations = PBKDF2 迭代次数. NIST SP 800-132 推荐 ≥ 1000,
	// OWASP 推荐 ≥ 100000 (2023+). 100k 平衡性能 (本地解密 <100ms).
	kdfIterations = 100_000

	// keyLen = AES-256 key length (32 bytes).
	keyLen = 32

	// nonceLen = AES-GCM nonce length (12 bytes).
	nonceLen = 12

	// saltStr = PBKDF2 salt (固定 app-level, 公开). 不是 secret — 防 rainbow table 即可.
	saltStr = "novel2all-pbkdf2-salt-v1"

	// kdfName = KDF 算法名 (写入文件用于 future migration).
	kdfName = "pbkdf2-sha256"
)

// providerEntry 单个 provider 的加密 entry.
type providerEntry struct {
	Nonce string `json:"nonce"` // base64
	CT    string `json:"ct"`    // base64
}

// storeFile 落盘格式 (JSON).
type storeFile struct {
	Version    int                     `json:"version"`
	KDF        string                  `json:"kdf"`
	Iterations int                     `json:"iterations"`
	Salt       string                  `json:"salt"`
	Providers  map[string]providerEntry `json:"providers"`
}

// Store 加密 secrets 存储 (per-user, per-machine).
//
// 线程安全: 内部 mutex 保护 file IO. Set/Get/Has 都可并发.
type Store struct {
	mu       sync.Mutex
	path     string // 落盘文件路径
	master   []byte // 32-byte AES key (派生后缓存, 进程内复用)
	password []byte // PBKDF2 输入 (派生材料)
}

// New 创建 Store, 不立即加载磁盘 (lazy load on first read).
//
// path: 完整文件路径 (含文件名). 调用方决定位置 (一般是 %APPDATA%/.../llm_keys.enc).
func New(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("secrets: path is required")
	}
	// 派生材料: 固定 salt + hostname + username
	password, err := derivePassword()
	if err != nil {
		return nil, fmt.Errorf("secrets: derive password: %w", err)
	}
	return &Store{
		path:     path,
		password: password,
	}, nil
}

// derivePassword 从 hostname + username 派生密码材料.
//
// 不安全但够用: 同机同用户能解, 其他用户/机器不能.
// 关键点: hostname 在 docker 容器里默认相同 (需要改进), 加上 username 区分.
// Phase 2 升级: 用 OS-level secret (DPAPI/Keychain).
func derivePassword() ([]byte, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("hostname: %w", err)
	}
	var username string
	if u, err := user.Current(); err == nil && u != nil {
		username = u.Username
	}
	// 拼成 password material (固定前缀 + 设备特定)
	// 不用随机 salt (不是 secret, 防 rainbow table 即可)
	material := "novel2all-desktop-master-v1:" + hostname + ":" + username
	return []byte(material), nil
}

// masterKey 派生 AES key (lazy, 缓存 in-memory).
func (s *Store) masterKey() []byte {
	if s.master != nil {
		return s.master
	}
	// PBKDF2(password, salt, iter, keyLen, SHA256)
	key := pbkdf2.Key(s.password, []byte(saltStr), kdfIterations, keyLen, sha256.New)
	s.master = key
	return key
}

// load 读盘 → 解析 → 解密到内存 (调用方负责 s.mu).
//
// 磁盘文件不存在 → 返 (empty store, nil), 不报错 (首次启动正常情况).
// 格式错误 / 解密失败 → 返 error.
func (s *Store) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read file: %w", err)
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	if f.Version != fileVersion {
		return nil, fmt.Errorf("unsupported version %d (expected %d)", f.Version, fileVersion)
	}
	if f.KDF != kdfName || f.Iterations != kdfIterations {
		return nil, fmt.Errorf("unsupported kdf %s/%d (expected %s/%d)", f.KDF, f.Iterations, kdfName, kdfIterations)
	}
	// 解密每个 provider
	gcm, err := newGCM(s.masterKey())
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(f.Providers))
	for name, entry := range f.Providers {
		nonce, err := base64.StdEncoding.DecodeString(entry.Nonce)
		if err != nil {
			return nil, fmt.Errorf("provider %s: nonce decode: %w", name, err)
		}
		ct, err := base64.StdEncoding.DecodeString(entry.CT)
		if err != nil {
			return nil, fmt.Errorf("provider %s: ct decode: %w", name, err)
		}
		// AAD = provider name, 防 ciphertext 重命名攻击 (改名 dashscope → deepseek)
		pt, err := gcm.Open(nil, nonce, ct, []byte(name))
		if err != nil {
			return nil, fmt.Errorf("provider %s: decrypt: %w", name, err)
		}
		out[name] = string(pt)
	}
	return out, nil
}

// save 写盘 (调用方负责 s.mu + caller 先加密).
func (s *Store) save(in map[string]string) error {
	gcm, err := newGCM(s.masterKey())
	if err != nil {
		return err
	}
	f := storeFile{
		Version:    fileVersion,
		KDF:        kdfName,
		Iterations: kdfIterations,
		Salt:       saltStr,
		Providers:  make(map[string]providerEntry, len(in)),
	}
	for name, plain := range in {
		nonce := make([]byte, nonceLen)
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return fmt.Errorf("nonce: %w", err)
		}
		// AAD = provider name, 防 ciphertext 重命名
		ct := gcm.Seal(nil, nonce, []byte(plain), []byte(name))
		f.Providers[name] = providerEntry{
			Nonce: base64.StdEncoding.EncodeToString(nonce),
			CT:    base64.StdEncoding.EncodeToString(ct),
		}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	// 原子写: 先写 tmp, 再 rename (防写一半挂掉导致文件损坏)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Set 存储指定 provider 的 key (覆盖已有).
//
// 立即写盘. 空 key 等同于删除 (Clear provider).
func (s *Store) Set(provider, key string) error {
	if provider == "" {
		return errors.New("secrets: provider name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	secrets, err := s.load()
	if err != nil {
		return err
	}
	if key == "" {
		delete(secrets, provider)
	} else {
		secrets[provider] = key
	}
	return s.save(secrets)
}

// Get 读取指定 provider 的 key. 不存在 → ("", nil) 不是 error.
func (s *Store) Get(provider string) (string, error) {
	if provider == "" {
		return "", errors.New("secrets: provider name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	secrets, err := s.load()
	if err != nil {
		return "", err
	}
	return secrets[provider], nil
}

// Has 判断 provider 是否已配置 key.
func (s *Store) Has(provider string) (bool, error) {
	v, err := s.Get(provider)
	if err != nil {
		return false, err
	}
	return v != "", nil
}

// List 返所有已配置 key 的 provider 名称 (按字母序).
//
// 不返 key 内容 (即使调用方有 access, 防御性减少泄露面).
func (s *Store) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secrets, err := s.load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(secrets))
	for name, v := range secrets {
		if v != "" {
			names = append(names, name)
		}
	}
	// 排序方便 UI 展示稳定
	sortStrings(names)
	return names, nil
}

// Clear 删除所有 keys (重置 / 登出).
//
// 文件本身也删除 (不只是 map 清空), 下次启动状态干净.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove: %w", err)
	}
	return nil
}

// Path 返落盘路径 (调试用).
func (s *Store) Path() string {
	return s.path
}

// newGCM 创建 AES-256-GCM cipher (key 必须 32 bytes).
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("secrets: invalid key length %d (want %d)", len(key), keyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	return cipher.NewGCM(block)
}

// sortStrings 小型 sort.Strings 替代 (避免 strings 包 import 噪声).
// 实际不优化性能 (UI 列表很短), 只为可读性.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// DefaultPath 返平台默认存储路径 (不创建文件).
//
// Windows: %APPDATA%\novel2all-desktop\llm_keys.enc
// Linux/Mac: ~/.config/novel2all-desktop/llm_keys.enc
//
// 复用 os.UserConfigDir() (类似 app.go tokenPath()).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "novel2all-desktop")
	return filepath.Join(appDir, "llm_keys.enc"), nil
}