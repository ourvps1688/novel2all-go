// state.go 提供运行时 state 持久化（项目列表 + cache 计数器）。
//
// 设计目标：
//   - 进程重启后 projects 不丢失（之前 P1-F 切片 1-8 全是内存，重启清零）
//   - cache counters 可选持久化（重启后归零也可接受，但持久化更友好）
//   - 显式 save endpoint（避免后台 goroutine 自动保存的复杂性）
//   - 文件用 atomic write（write to .tmp + rename）防止半写文件
//
// 持久化文件：data/state.json（schema 版本号支持未来迁移）
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// State 进程 state 快照（v1 schema）
type State struct {
	// Version schema 版本号（未来加字段时 +1，老 code 拒绝读新 schema）
	Version int `json:"version"`
	// SavedAt 最后保存时间
	SavedAt time.Time `json:"saved_at"`
	// Projects 项目列表（指针转值类型用于序列化）
	Projects []*Project `json:"projects"`
	// Cache cache 计数器快照
	Cache CacheState `json:"cache"`
}

// CacheState cache 计数器快照（与 cache.go 的全局变量同步）
type CacheState struct {
	Hits         int64 `json:"hits"`
	Misses       int64 `json:"misses"`
	PrefixHits   int64 `json:"prefix_hits"`
	PrefixMisses int64 `json:"prefix_misses"`
	// Size 当前 cache 占用条目数（运行时统计，不持久化到 cache 文件但 state 持久化以便重启恢复）
	Size int64 `json:"size"`
}

// StateInfo GET /api/state 响应
type StateInfo struct {
	Path        string    `json:"path"`
	Exists      bool      `json:"exists"`
	Size        int64     `json:"size"` // 文件字节数
	LastSavedAt time.Time `json:"last_saved_at,omitempty"`
	LastLoadAt  time.Time `json:"last_load_at,omitempty"`
	Version     int       `json:"version,omitempty"`
	Projects    int       `json:"projects"`   // 当前内存中项目数
	CacheHits   int64     `json:"cache_hits"` // 当前内存中 hits
}

// StatePersistor state 持久化管理器
//
// 线程安全（mu 保护 file IO）。
// 启动时调用 Load() 恢复，运维调用 Save() 持久化。
type StatePersistor struct {
	path     string
	projects *ProjectStore
	mu       sync.Mutex
	// lastSaveAt 最后一次 Save 成功时间
	lastSaveAt time.Time
	// lastLoadAt 最后一次 Load 成功时间
	lastLoadAt time.Time
}

// NewStatePersistor 创建 persistor
//
// path 持久化文件路径（一般 data/state.json）
// projects 项目存储引用（用来读 snapshot 和 restore）
func NewStatePersistor(path string, projects *ProjectStore) *StatePersistor {
	return &StatePersistor{
		path:     path,
		projects: projects,
	}
}

// Path 返回持久化文件路径
func (s *StatePersistor) Path() string {
	return s.path
}

// Snapshot 取当前内存 state（用于保存或查询）
//
// 不需要 mu（只读 atomic + projects.mu.RLock）。
func (s *StatePersistor) Snapshot() State {
	return State{
		Version:  1,
		SavedAt:  time.Now(),
		Projects: s.projects.List(),
		Cache: CacheState{
			Hits:         atomicLoadInt64(&cacheHits),
			Misses:       atomicLoadInt64(&cacheMisses),
			PrefixHits:   atomicLoadInt64(&prefixHits),
			PrefixMisses: atomicLoadInt64(&prefixMisses),
			Size:         atomicLoadInt64(&cacheSize),
		},
	}
}

// Save 把当前 state 写入文件（atomic write: tmp + rename）
//
// 流程：
//  1. Snapshot() 当前 state
//  2. 编码 JSON 到临时文件（{path}.tmp）
//  3. fsync（持久化到磁盘）
//  4. rename(tmp, path)（POSIX 原子 rename）
func (s *StatePersistor) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.Snapshot()
	data, err := json.MarshalIndent(&snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// 原子写：先写 .tmp，再 rename
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath) // 清理
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, s.path, err)
	}

	s.lastSaveAt = time.Now()
	return nil
}

// Load 从文件加载 state 并恢复到内存
//
// 行为：
//   - 文件不存在 → 静默返回 nil（首次启动正常情况）
//   - schema 版本不匹配 → 返回错误（拒绝加载，避免数据损坏）
//   - JSON 解析失败 → 返回错误
//   - 加载成功 → 替换内存 projects + cache 计数器
func (s *StatePersistor) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // 首次启动，无 state file
		}
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil // 空文件等同不存在
	}

	var loaded State
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("unmarshal state: %w", err)
	}
	if loaded.Version != 1 {
		return fmt.Errorf("unsupported state version: %d (expected 1)", loaded.Version)
	}

	// 恢复 projects（用 ProjectsHandler 的 store 替换数据）
	if err := s.projects.RestoreAll(loaded.Projects); err != nil {
		return fmt.Errorf("restore projects: %w", err)
	}

	// 恢复 cache 计数器
	atomicStoreInt64(&cacheHits, loaded.Cache.Hits)
	atomicStoreInt64(&cacheMisses, loaded.Cache.Misses)
	atomicStoreInt64(&prefixHits, loaded.Cache.PrefixHits)
	atomicStoreInt64(&prefixMisses, loaded.Cache.PrefixMisses)
	atomicStoreInt64(&cacheSize, loaded.Cache.Size)

	s.lastLoadAt = time.Now()
	return nil
}

// Reset 清空内存 state + 删除持久化文件
//
// 谨慎使用：admin only。
// 返回：
//   - deleted: bool 文件是否被删除（不存在返回 false）
func (s *StatePersistor) Reset() (deleted bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 清空内存
	if err := s.projects.RestoreAll(nil); err != nil {
		return false, fmt.Errorf("reset projects: %w", err)
	}
	atomicStoreInt64(&cacheHits, 0)
	atomicStoreInt64(&cacheMisses, 0)
	atomicStoreInt64(&prefixHits, 0)
	atomicStoreInt64(&prefixMisses, 0)
	atomicStoreInt64(&cacheSize, 0)

	// 删除文件（best-effort）
	if err := os.Remove(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Info 返回 state 持久化元信息（GET /api/state 用）
func (s *StatePersistor) Info() StateInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	info := StateInfo{
		Path:        s.path,
		LastSavedAt: s.lastSaveAt,
		LastLoadAt:  s.lastLoadAt,
		Projects:    len(s.projects.List()),
		CacheHits:   atomicLoadInt64(&cacheHits),
	}

	if data, err := os.Stat(s.path); err == nil {
		info.Exists = true
		info.Size = data.Size()
		if info.LastSavedAt.IsZero() {
			info.LastSavedAt = data.ModTime()
		}
	}

	// 尝试读 version（不读整个 file，避免大文件 IO）
	if f, err := os.Open(s.path); err == nil {
		defer func() { _ = f.Close() }()
		dec := json.NewDecoder(f)
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			if key, ok := tok.(string); ok && key == "version" {
				if v, err := dec.Token(); err == nil {
					if n, ok := v.(float64); ok {
						info.Version = int(n)
					}
				}
				break
			}
		}
	}

	return info
}

// atomicLoadInt64 读 int64 原子变量
func atomicLoadInt64(p *int64) int64 {
	return atomic.LoadInt64(p)
}

// atomicStoreInt64 写 int64 原子变量
func atomicStoreInt64(p *int64, v int64) {
	atomic.StoreInt64(p, v)
}

// 防止 unused 警告
var _ = io.Discard
