//go:build windows

// autostart_windows.go - Windows Registry 操作 (Module J 自启).
// 用 golang.org/x/sys/windows/registry 写 HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
//
// 不在 macOS/Linux 构建 (build tag 限制).
package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const (
	// autostartKeyPath HKCU\Software\Microsoft\Windows\CurrentVersion\Run
	// (用户级 Run, 不需要 admin 权限)
	autostartKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValueName = "Novel2ALL"
)

// EnableAutoStart 添加 Windows Registry 自启项.
//
// 值: 当前 exe 完整路径. 卸载时调用 DisableAutoStart 清理.
func EnableAutoStart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	// 用引号包路径 (空格或特殊字符支持)
	value := fmt.Sprintf(`"%s"`, filepath.Clean(exe))

	k, _, err := registry.CreateKey(registry.CURRENT_USER, autostartKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create registry key: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(autostartValueName, value); err != nil {
		return fmt.Errorf("set registry value: %w", err)
	}
	return nil
}

// DisableAutoStart 删除 Windows Registry 自启项 (如果存在).
// 幂等: 不存在也不报错.
func DisableAutoStart() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil // 已经不存在, 算成功
		}
		return fmt.Errorf("open registry key: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(autostartValueName); err != nil {
		// "system cannot find the file specified" → 值不存在, 算成功
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("delete registry value: %w", err)
	}
	return nil
}

// isAutoStartEnabled 检查当前 Windows Registry 自启项是否存在.
func isAutoStartEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil // key 不存在 = 未启用
		}
		return false, err
	}
	defer k.Close()

	_, _, err = k.GetStringValue(autostartValueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
