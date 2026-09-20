//go:build !windows

// autostart_other.go - macOS/Linux stub (Module J 自启).
// 暂不支持 macOS/Linux 自动启动 (后续 Sprint 可加 LaunchAgents / .desktop 文件).

package settings

import "errors"

// EnableAutoStart 在非 Windows 平台不可用.
func EnableAutoStart() error {
	return errors.New("auto-start not supported on this OS (Windows only in this sprint)")
}

// DisableAutoStart 在非 Windows 平台不可用 (但幂等返 nil).
func DisableAutoStart() error {
	return nil
}

// isAutoStartEnabled 在非 Windows 平台永远返 false.
func isAutoStartEnabled() (bool, error) {
	return false, nil
}
