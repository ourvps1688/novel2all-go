// backup.go 提供运行时 state 备份到 tar.gz（P1-F 切片 12）。
//
// 备份内容（tar.gz 内文件）：
//   - manifest.json   元信息（版本、创建时间、内容列表、checksum）
//   - state.json      进程 state 快照（projects + cache counters）
//   - db.sqlite       SQLite 数据库文件（如果存在）
//
// 设计要点：
//   - atomic write: 写 .tmp + rename（POSIX atomic rename）
//   - auto cleanup: 保留最新 maxBackups 个，超出删除最旧
//   - manifest 包含 SHA256 checksum（验证完整性）
package store

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupManifest 备份元信息（tar.gz 内的 manifest.json）
type BackupManifest struct {
	Version   string            `json:"version"`    // schema 版本号
	CreatedAt time.Time         `json:"created_at"` // 备份创建时间
	Hostname  string            `json:"hostname,omitempty"`
	Contents  []string          `json:"contents"`  // 文件列表（state.json, db.sqlite 等）
	Checksums map[string]string `json:"checksums"` // filename → SHA256
}

// BackupInfo 单个 backup 文件的元信息（GET /api/backups 用）
type BackupInfo struct {
	Filename  string    `json:"filename"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"` // 字节数
	CreatedAt time.Time `json:"created_at"`
	Hostname  string    `json:"hostname,omitempty"`
	Contents  []string  `json:"contents"`
}

// BackupResult POST /api/backup 响应
type BackupResult struct {
	Filename  string    `json:"filename"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Contents  []string  `json:"contents"`
	Message   string    `json:"message,omitempty"`
}

// BackupManager 备份管理器
type BackupManager struct {
	dir        string // 备份目录（如 data/backups/）
	statePath  string // state.json 路径（如 data/state.json）
	dbPath     string // sqlite db 路径（如 data/novel2all.db）
	maxBackups int    // 保留最近 N 个
}

// NewBackupManager 创建备份管理器
//
// dir 备份目录（不存在自动创建）
// statePath state.json 路径（用于复制）
// dbPath sqlite db 路径（用于复制，可选不存在）
// maxBackups 保留最近 N 个（0 或负数 = 保留全部）
func NewBackupManager(dir, statePath, dbPath string, maxBackups int) *BackupManager {
	return &BackupManager{
		dir:        dir,
		statePath:  statePath,
		dbPath:     dbPath,
		maxBackups: maxBackups,
	}
}

// Dir 返回备份目录路径
func (b *BackupManager) Dir() string {
	return b.dir
}

// Create 创建新备份，返回 BackupResult
//
// 流程：
//  1. 生成 timestamped filename
//  2. 写 manifest + state + db 到 .tmp tar.gz
//  3. atomic rename 到最终路径
//  4. auto cleanup 旧 backup
func (b *BackupManager) Create() (*BackupResult, error) {
	// 确保备份目录存在
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", b.dir, err)
	}

	// 生成 filename（含纳秒确保唯一，避免同秒重复创建覆盖）
	timestamp := time.Now().UTC().Format("20060102-150405.000000000")
	filename := fmt.Sprintf("novel2all-%s.tar.gz", timestamp)
	finalPath := filepath.Join(b.dir, filename)
	tmpPath := finalPath + ".tmp"

	// 创建 .tmp tar.gz
	if err := b.writeTarGz(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	// atomic rename
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("rename %s -> %s: %w", tmpPath, finalPath, err)
	}

	// 取文件信息
	fi, err := os.Stat(finalPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", finalPath, err)
	}

	result := &BackupResult{
		Filename:  filename,
		Path:      finalPath,
		Size:      fi.Size(),
		CreatedAt: fi.ModTime().UTC(),
		Message:   "backup created successfully",
	}

	// auto cleanup（保留最近 maxBackups）
	if err := b.cleanup(); err != nil {
		// cleanup 失败不阻塞 backup 返回
		result.Message = fmt.Sprintf("backup created (cleanup warning: %v)", err)
	}

	// 取 manifest 内容（再次读 manifest）
	if manifest, _ := b.readManifest(finalPath); manifest != nil {
		result.Contents = manifest.Contents
	}

	return result, nil
}

// List 列出所有 backup（按时间倒序）
func (b *BackupManager) List() ([]BackupInfo, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupInfo{}, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", b.dir, err)
	}

	var backups []BackupInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		bi := BackupInfo{
			Filename:  e.Name(),
			Path:      filepath.Join(b.dir, e.Name()),
			Size:      info.Size(),
			CreatedAt: info.ModTime().UTC(),
		}
		// 尝试读 manifest 获取 Contents
		if manifest, _ := b.readManifest(bi.Path); manifest != nil {
			bi.Contents = manifest.Contents
			bi.Hostname = manifest.Hostname
		}
		backups = append(backups, bi)
	}

	// 按创建时间倒序
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// writeTarGz 写 manifest + state + db 到 tar.gz
func (b *BackupManager) writeTarGz(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	manifest := BackupManifest{
		Version:   "1",
		CreatedAt: time.Now().UTC(),
		Contents:  []string{},
		Checksums: make(map[string]string),
	}

	// 1. 写 state.json
	if err := b.addFile(tw, &manifest, "state.json", b.statePath); err != nil {
		return fmt.Errorf("add state.json: %w", err)
	}

	// 2. 写 db.sqlite（如果存在）
	if b.dbPath != "" {
		if _, err := os.Stat(b.dbPath); err == nil {
			if err := b.addFile(tw, &manifest, "db.sqlite", b.dbPath); err != nil {
				return fmt.Errorf("add db.sqlite: %w", err)
			}
		}
	}

	// 3. 写 manifest.json（最后写，含其他文件 checksum）
	if err := b.writeManifestEntry(tw, &manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	return nil
}

// addFile 添加单个文件到 tar.gz
func (b *BackupManager) addFile(tw *tar.Writer, manifest *BackupManifest, name, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// 计算 checksum
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])

	// tar header
	header := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write data %s: %w", name, err)
	}

	// 更新 manifest
	manifest.Contents = append(manifest.Contents, name)
	manifest.Checksums[name] = checksum

	return nil
}

// writeManifestEntry 写 manifest.json 到 tar.gz
func (b *BackupManager) writeManifestEntry(tw *tar.Writer, manifest *BackupManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	header := &tar.Header{
		Name:    "manifest.json",
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write manifest header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write manifest data: %w", err)
	}
	return nil
}

// readManifest 读 backup 文件中的 manifest.json
func (b *BackupManager) readManifest(path string) (*BackupManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("manifest not found in %s", path)
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == "manifest.json" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			var manifest BackupManifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, err
			}
			return &manifest, nil
		}
	}
}

// cleanup 保留最新 maxBackups 个，删除其余
func (b *BackupManager) cleanup() error {
	if b.maxBackups <= 0 {
		return nil // 0 = 保留全部
	}
	backups, err := b.List()
	if err != nil {
		return err
	}
	if len(backups) <= b.maxBackups {
		return nil
	}
	// 删除最旧的（List 已按时间倒序，跳过前 maxBackups）
	for _, old := range backups[b.maxBackups:] {
		if err := os.Remove(old.Path); err != nil {
			return fmt.Errorf("remove %s: %w", old.Path, err)
		}
	}
	return nil
}
