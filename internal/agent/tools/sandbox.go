package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Sandbox 沙箱（所有文件操作必须验证 path 在 root 下）
type Sandbox interface {
	// Validate 检查 path 是否在沙箱内，返回 error 否则
	Validate(path string) error
	// Root 返回沙箱根目录
	Root() string
	// Resolve 把相对路径解析为绝对路径（必须以 root 为前缀）
	Resolve(path string) (string, error)
}

// rootSandbox 基于 root 目录的沙箱
type rootSandbox struct {
	root string
}

// NewRootSandbox 创建基于 root 目录的沙箱
func NewRootSandbox(root string) Sandbox {
	abs, _ := filepath.Abs(root)
	return &rootSandbox{root: abs}
}

// Root 返回沙箱根目录
func (s *rootSandbox) Root() string {
	return s.root
}

// Resolve 把 path 解析为绝对路径，验证在沙箱内
func (s *rootSandbox) Resolve(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("sandbox: empty path")
	}

	// 拒绝 .. 逃避
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("sandbox: path contains '..': %s", path)
	}

	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		// 相对路径，相对于 root
		absPath = filepath.Clean(filepath.Join(s.root, path))
	}

	if err := s.Validate(absPath); err != nil {
		return "", err
	}
	return absPath, nil
}

// Validate 验证 path 在 root 下
func (s *rootSandbox) Validate(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("sandbox: resolve abs: %w", err)
	}
	abs = filepath.Clean(abs)

	// 必须以 root + separator 开头（避免 /root_evil 匹配 /root）
	rootWithSep := s.root
	if !strings.HasSuffix(rootWithSep, string(filepath.Separator)) {
		rootWithSep += string(filepath.Separator)
	}
	if !strings.HasPrefix(abs, rootWithSep) && abs != s.root {
		return fmt.Errorf("sandbox: path %q escapes root %q", abs, s.root)
	}
	return nil
}
