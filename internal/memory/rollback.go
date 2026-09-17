// Package memory 提供 novel2all-go 长记忆系统.
package memory

// rollback.go 实现 WriteSnapshot + RollbackManager (Sprint 24).
//
// 设计:
//   - Snapshot: 保存 chapter 写作前的 state JSON (写到 data/_snapshots/ch{N}.json)
//   - RollbackManager: 管理 snapshots + 回滚 (恢复到 snapshot state)
//   - 每个 snapshot 含完整 state, 回滚覆盖当前 state.json
//   - 不做增量 StateChange 记录 (Python V0.30.6 用 StateChange, V0 简化用全量 snapshot)
//
// 参考 Python V0.30.6 B2 core/memory/rollback.py.
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// WriteSnapshot 单次写作前的 state 快照.
type WriteSnapshot struct {
	Chapter      int            `json:"chapter"`
	State        *TrackingState `json:"state"`
	SnapshotPath string         `json:"snapshot_path"`
	CreatedAt    time.Time      `json:"created_at"`
}

// RollbackResult 回滚结果.
type RollbackResult struct {
	Chapter     int    `json:"chapter"`
	Restored    bool   `json:"restored"`
	NewStateLen int    `json:"new_state_len"`
	Message     string `json:"message"`
}

// RollbackManager 快照管理 + 回滚.
type RollbackManager struct {
	projectRoot  string
	snapshotDir  string // data/_snapshots/
	maxSnapshots int    // 保留最近 N 个 snapshot
}

// NewRollbackManager 创建.
//
// maxSnapshots: 0 = 保留所有.
func NewRollbackManager(projectRoot string, maxSnapshots int) *RollbackManager {
	return &RollbackManager{
		projectRoot:  projectRoot,
		snapshotDir:  filepath.Join(projectRoot, "data", "_snapshots"),
		maxSnapshots: maxSnapshots,
	}
}

// SnapshotDir 返回 snapshot 目录 (暴露给 manager 用).
func (rm *RollbackManager) SnapshotDir() string {
	return rm.snapshotDir
}

// Snapshot 创建 chapter 写作前的快照.
//
// 注意: 这里对 state 做 deep copy (序列化+反序列化), 避免 caller 修改 state 后影响 snapshot.
func (rm *RollbackManager) Snapshot(chapter int, state *TrackingState) (*WriteSnapshot, error) {
	if err := os.MkdirAll(rm.snapshotDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir snapshot dir: %w", err)
	}

	// deep copy via JSON roundtrip (V0 简化, Sprint 25+ 可换 snapshot pattern)
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}
	var stateCopy TrackingState
	if err := json.Unmarshal(data, &stateCopy); err != nil {
		return nil, fmt.Errorf("unmarshal state copy: %w", err)
	}

	snap := &WriteSnapshot{
		Chapter:      chapter,
		State:        &stateCopy,
		SnapshotPath: filepath.Join(rm.snapshotDir, fmt.Sprintf("ch%d.json", chapter)),
		CreatedAt:    time.Now().UTC(),
	}

	data2, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}

	// 写到 tmp + rename
	tmp := snap.SnapshotPath + ".tmp"
	if err := os.WriteFile(tmp, data2, 0o644); err != nil {
		return nil, fmt.Errorf("write snapshot: %w", err)
	}
	if err := os.Rename(tmp, snap.SnapshotPath); err != nil {
		return nil, fmt.Errorf("rename snapshot: %w", err)
	}

	// cleanup old
	_ = rm.cleanupOldBackups()

	return snap, nil
}

// Rollback 恢复到 snapshot state.
//
// 覆盖当前 state.json (写回到 tracker 的 stateFile).
func (rm *RollbackManager) Rollback(snap *WriteSnapshot, tracker *Tracker) (*RollbackResult, error) {
	if snap == nil || snap.State == nil {
		return &RollbackResult{
			Chapter:  0,
			Restored: false,
			Message:  "nil snapshot",
		}, fmt.Errorf("nil snapshot")
	}

	// 写回到 tracker (用 snapshot.State 直接覆盖)
	if err := tracker.Write(snap.State); err != nil {
		return &RollbackResult{
			Chapter:  snap.Chapter,
			Restored: false,
			Message:  fmt.Sprintf("write failed: %v", err),
		}, err
	}

	return &RollbackResult{
		Chapter:     snap.Chapter,
		Restored:    true,
		NewStateLen: len(snap.State.Characters) + len(snap.State.Foreshadowing),
		Message:     fmt.Sprintf("rolled back to chapter %d state", snap.Chapter),
	}, nil
}

// LoadSnapshot 从 disk 加载 snapshot.
func (rm *RollbackManager) LoadSnapshot(chapter int) (*WriteSnapshot, error) {
	fp := filepath.Join(rm.snapshotDir, fmt.Sprintf("ch%d.json", chapter))
	data, err := os.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("read snapshot %d: %w", chapter, err)
	}
	var snap WriteSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return &snap, nil
}

// ListSnapshots 列出所有 snapshot chapters (按 chapter ASC).
func (rm *RollbackManager) ListSnapshots() ([]int, error) {
	if _, err := os.Stat(rm.snapshotDir); err != nil {
		if os.IsNotExist(err) {
			return []int{}, nil
		}
		return nil, err
	}

	entries, err := os.ReadDir(rm.snapshotDir)
	if err != nil {
		return nil, err
	}
	var chapters []int
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var ch int
		if _, err := fmt.Sscanf(e.Name(), "ch%d.json", &ch); err == nil {
			chapters = append(chapters, ch)
		}
	}
	sort.Ints(chapters)
	return chapters, nil
}

// cleanupOldBackups 删除最旧的 snapshots, 只保留 maxSnapshots 个.
func (rm *RollbackManager) cleanupOldBackups() error {
	if rm.maxSnapshots <= 0 {
		return nil // 0 = 不限制
	}
	chapters, err := rm.ListSnapshots()
	if err != nil {
		return err
	}
	if len(chapters) <= rm.maxSnapshots {
		return nil
	}

	// 删除最旧的
	toDelete := len(chapters) - rm.maxSnapshots
	for i := 0; i < toDelete; i++ {
		fp := filepath.Join(rm.snapshotDir, fmt.Sprintf("ch%d.json", chapters[i]))
		if err := os.Remove(fp); err != nil {
			return err
		}
	}
	return nil
}
