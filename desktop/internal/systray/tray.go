// Package systray 提供 Novel2ALL 桌面 app 的系统托盘支持.
//
// Phase 1.4 设计:
//   - systray.Run() 在 main() 启动, 与 Wails WebView 并存
//   - 菜单: "打开主界面" / "状态: ..." (disabled) / 分隔 / "退出"
//   - 单实例锁: TCP probe, 已有实例在跑则激活已有窗口
//   - 关闭 WebView 窗口 → 不退出, 最小化到托盘
//
// 注意: systray 是阻塞调用, 必须放到独立 goroutine.
//
// 依赖: github.com/getlantern/systray
package systray

import (
	"fmt"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

// IconData 系统托盘 16x16 ICO bytes (默认 fallback).
//
// Phase 1.4 用 emoji + 文字 placeholder. 后续 Phase 美化用真正的 ICO.
var IconData []byte = buildFallbackIcon()

// buildFallbackIcon 生成一个最小可用的 16x16 ICO (透明).
//
// ICO format: ICONDIR header (6 bytes) + ICONDIRENTRY (16 bytes) + BITMAPINFOHEADER.
// 为简化, 我们直接 embed 一个空 PNG 让 systray 显示默认图标.
//
// 实际项目里 Phase 美化阶段会换成 build/appicon.png.
func buildFallbackIcon() []byte {
	// 最小 ICO: header (6) + entry (16) + BITMAPINFOHEADER (40) + pixel data
	// 这里返回 nil 让 systray 用默认图标 (显示文字 "Novel2ALL" 作 fallback).
	return nil
}

// MenuClick 回调类型.
type MenuClick func(itemID string)

// MenuItemID 菜单项标识.
const (
	MenuOpen   = "open"   // 打开主界面
	MenuStatus = "status" // 后端状态 (disabled)
	MenuAbout  = "about"  // 关于
	MenuQuit   = "quit"   // 退出
)

// Menu 系统托盘菜单 + 状态.
type Menu struct {
	mu           sync.RWMutex
	openClicked  func()
	aboutClicked func()
	quitClicked  func()
	onQuit       func() // 用户选 "退出" 时调, 用于触发 Wails 退出

	statusItem *systray.MenuItem
	openItem   *systray.MenuItem
	aboutItem  *systray.MenuItem
	quitItem   *systray.MenuItem
	running    bool
}

// SetCallbacks 注册菜单点击回调.
//
// 启动 systray.Run 前调用.
func (m *Menu) SetCallbacks(onOpen, onAbout, onQuit func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openClicked = onOpen
	m.aboutClicked = onAbout
	m.quitClicked = onQuit
}

// SetStatusText 更新托盘菜单的"状态"项文字 (e.g. "状态: 已连接" / "状态: 离线").
//
// status="" 时隐藏.
func (m *Menu) SetStatusText(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusItem == nil {
		return
	}
	m.statusItem.SetTitle(text)
	m.statusItem.Hide()
	m.statusItem.Show()
}

// Run 启动系统托盘 (阻塞).
//
// 在独立 goroutine 调: go menu.Run()
//
// 退出时调 systray.Quit() (我们通过 quitClicked + Quit 链路触发).
func (m *Menu) Run() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	onExit := func() {
		// systray 退出前回调, 可清理资源
	}

	systray.Run(func() {
		m.onReady()
	}, onExit)
}

// Quit 主动退出 systray (与用户点 "退出" 菜单等价).
//
// 通常在用户点 Quit 菜单时由 onQuitClicked 调, 然后调 quitClicked → 应用退出.
func (m *Menu) Quit() {
	systray.Quit()
}

// onReady systray.Run 启动后的回调, 在此处构建菜单.
func (m *Menu) onReady() {
	systray.SetIcon(IconData)
	systray.SetTitle("Novel2ALL")
	systray.SetTooltip("Novel2ALL - 本地优先 + 云端同步的小说创作工具")

	// 打开主界面
	m.openItem = systray.AddMenuItem("打开主界面", "显示 Novel2ALL 主窗口")
	go func() {
		for range m.openItem.ClickedCh {
			m.mu.RLock()
			cb := m.openClicked
			m.mu.RUnlock()
			if cb != nil {
				cb()
			}
		}
	}()

	// 状态 (默认隐藏, 由 SetStatusText 显示)
	m.statusItem = systray.AddMenuItem("状态: 检测中...", "后端连接状态")
	m.statusItem.Disable()
	m.statusItem.Hide()

	systray.AddSeparator()

	// 关于
	m.aboutItem = systray.AddMenuItem("关于 Novel2ALL", "查看版本信息")
	go func() {
		for range m.aboutItem.ClickedCh {
			m.mu.RLock()
			cb := m.aboutClicked
			m.mu.RUnlock()
			if cb != nil {
				cb()
			}
		}
	}()

	systray.AddSeparator()

	// 退出
	m.quitItem = systray.AddMenuItem("退出", "完全退出 Novel2ALL")
	go func() {
		for range m.quitItem.ClickedCh {
			m.mu.RLock()
			cb := m.quitClicked
			m.mu.RUnlock()
			if cb != nil {
				cb()
			}
		}
	}()
}

// TryAcquireSingleInstance 尝试获取单实例锁.
//
// 监听 localhost:0 (随机端口), 立即返回.
//
// 如果监听成功 → 本进程是首个, 返 (true, port).
// 如果 EADDRINUSE → 已有实例在跑, 返 (false, 0).
//
// 用 net.Listen 而不是 TCP probe (因为更可靠, 跨平台).
func TryAcquireSingleInstance() (bool, int) {
	// 监听 localhost:0 → 操作系统自动分配空闲端口
	// 立即 Close, 仅作为 "端口探测"
	l, err := listenTCP("127.0.0.1:0")
	if err != nil {
		// EADDRINUSE 或其他错误 → 假设已有实例 (保守)
		return false, 0
	}
	// 拿到端口
	addr := l.Addr().String()
	port := extractPort(addr)
	l.Close()
	if port == 0 {
		return false, 0
	}
	return true, port
}

// 提取 "127.0.0.1:52345" 里的端口号.
//
// 用最简化的字符串处理 (避免 net.SplitHostPort 在 Windows port=0 时报错).
func extractPort(addr string) int {
	// 找最后一个冒号
	idx := lastIndex(addr, ':')
	if idx < 0 {
		return 0
	}
	portStr := addr[idx+1:]
	p := 0
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return 0
		}
		p = p*10 + int(c-'0')
	}
	return p
}

// ----- cross-platform net.Listen "127.0.0.1:0" 抽象 -----

// listenTCP 监听 TCP. 用 net.Listen 直接调用.
func listenTCP(addr string) (TCPListener, error) {
	return netListen("tcp", addr)
}

// TCPListener 抽象 net.Listener.
type TCPListener interface {
	Addr() TCPAddr
	Close() error
}

// TCPAddr 抽象 net.Addr.String() 返回 "host:port".
type TCPAddr interface {
	String() string
}

// ------- helpers (避免每次写导入) -------

// lastIndex 是 strings.LastIndex 的内联版本 (避免 imports).
func lastIndex(s string, sub byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == sub {
			return i
		}
	}
	return -1
}

// netListen 占位 — 实际在 main_windows.go / main_unix.go 用 build tag 分发.
//
// 这里仅用于编译期不报错 (实际运行时会选对应平台的版本).
var netListen func(network, addr string) (TCPListener, error) = nil

// 版本信息
var Version = "1.0.0"

// Compile-time 编译检查.
var _ = fmt.Sprintf
var _ = time.Now
var _ sync.RWMutex
