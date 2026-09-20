// Package settings 持久化桌面 app 配置 (Module J - 2026-09-20).
//
// 设计: 简单 JSON 文件在 %APPDATA%/novel2all-desktop/settings.json.
// 跨平台: Windows 用 golang.org/x/sys/windows/registry 写 HKCU\...\Run 自动启动;
// macOS/Linux 暂不支持 (可后续加 LaunchAgents / .desktop 文件).
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Settings 持久化的桌面 app 配置.
type Settings struct {
	// AutoStart 开机自启 (Windows HKCU\...\Run).
	AutoStart bool `json:"auto_start"`

	// StartMinimized 启动时最小化到托盘 (不弹主窗口).
	StartMinimized bool `json:"start_minimized"`
}

// Default 返回默认设置 (AutoStart false, StartMinimized false).
func Default() Settings {
	return Settings{
		AutoStart:      false,
		StartMinimized: false,
	}
}

// settingsFileName 配置文件名.
const settingsFileName = "settings.json"

// settingsDir 桌面 app 配置目录.
func settingsDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "novel2all-desktop")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		return "", err
	}
	return appDir, nil
}

// SettingsPath 配置文件完整路径.
func SettingsPath() (string, error) {
	d, err := settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, settingsFileName), nil
}

// Load 读配置文件. 文件不存在返 Default() (无错).
func Load() (Settings, error) {
	path, err := SettingsPath()
	if err != nil {
		return Default(), err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Default(), err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		// 文件损坏 (用户手改过? etc). 用默认 + 返错让上层决定.
		return Default(), err
	}
	return s, nil
}

// Save 原子写配置文件 (tmp + rename).
func Save(s Settings) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

// IsAutoStartEnabled 检查 Windows Registry 当前状态.
//
// 注意: 这反映系统实际状态, 不一定是用户在我 app 里的设置 (可能用户开了
// 但 registry 写失败, 或 app 卸载了但 registry 还在).
//
// 返回: 当前是否启用自启 + 错误.
func IsAutoStartEnabled() (bool, error) {
	return isAutoStartEnabled()
}
