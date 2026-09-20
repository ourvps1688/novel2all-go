package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"desktop-novel2all/internal/settings"
	"desktop-novel2all/internal/systray"
)

//go:embed all:frontend/dist
var assets embed.FS

// instanceLock 单实例锁的 TCP 端口 (0 = 失败).
//
// 监听 localhost:instanceLockPort. 如果端口已被占用 → 已有实例在跑.
var instanceLockPort int

// trayMenu 全局 systray 菜单引用.
var trayMenu = &systray.Menu{}

// trayReady systray 启动完成标志.
var trayReady = make(chan struct{})

// main 启动顺序:
//  1. 单实例锁检测 (TryAcquireSingleInstance)
//  2. 失败 → 通知已有实例 + 退出
//  3. 成功 → 启动 systray goroutine
//  4. 启动 Wails app (阻塞, 直到用户退出)
func main() {
	// 1. 单实例锁
	got, port := systray.TryAcquireSingleInstance()
	if !got {
		fmt.Fprintln(os.Stderr, "Novel2ALL 已在运行 (端口被占用). 退出.")
		os.Exit(1)
	}
	instanceLockPort = port
	log.Printf("[novel2all] acquired single-instance lock on port %d", port)

	// 2. 启动 systray (独立 goroutine, 不阻塞 main)
	go runTray()

	// 3. 启动 Wails app (阻塞)
	app := NewApp()
	app.trayMenuRef = trayMenu

	// Module J (2026-09-20): 启动时根据 settings 决定是否 StartHidden.
	startSettings, _ := settings.Load()
	startHidden := startSettings.StartMinimized

	err := wails.Run(&options.App{
		Title:             "Novel2ALL",
		Width:             1024,
		Height:            768,
		MinWidth:          800,
		MinHeight:         600,
		HideWindowOnClose: true, // 关窗 → 最小化到托盘, 不退出
		StartHidden:       startHidden,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 250, G: 250, B: 250, A: 1},
		OnStartup:        app.startup,
		OnBeforeClose:    app.onBeforeClose,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Printf("Error: %v", err.Error())
	}

	// 4. 退出时关闭 systray
	trayMenu.Quit()
}

// runTray 启动 systray 菜单 (阻塞在 systray.Run).
//
// 启动后会调用 setCallbacks 注册菜单点击事件.
func runTray() {
	trayMenu.SetCallbacks(
		onTrayOpen,  // "打开主界面"
		onTrayAbout, // "关于"
		onTrayQuit,  // "退出"
	)
	trayMenu.Run()
	close(trayReady)
}

// onTrayOpen "打开主界面" 菜单点击.
//
// 调 Wails runtime.WindowShow + Unminimize 把窗口拉到前台.
func onTrayOpen() {
	runtime.WindowShow(AppCtx)
	runtime.WindowUnminimise(AppCtx)
}

// onTrayAbout "关于" 菜单点击.
//
// Phase 1.4 简化: 用 Wails MessageDialog 弹出版本信息.
// Phase 1.5+: 换 settings page.
func onTrayAbout() {
	runtime.MessageDialog(AppCtx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   "关于 Novel2ALL",
		Message: fmt.Sprintf("Novel2ALL v%s\n\n本地优先 + 云端同步的小说创作工具.\n\n后端: https://api.zxc.im", systray.Version),
	})
}

// onTrayQuit "退出" 菜单点击.
//
// 触发 Wails app 退出 + 关闭 systray.
func onTrayQuit() {
	log.Printf("[novel2all] user clicked Quit, shutting down")
	runtime.Quit(AppCtx)
}

// ----------------------------------------------------------------------------
// App extensions for systray integration
// ----------------------------------------------------------------------------

// AppCtx 是 Wails 启动时传入的 context, 用于 systray 菜单操作 (runtime.*).
var AppCtx context.Context

// onBeforeClose Wails 窗口关闭前回调.
//
// 因为 HideWindowOnClose=true, 窗口关闭不会退出 app, 只隐藏.
// 这个回调在 hide 之前触发, 我们可以更新托盘状态.
func (a *App) onBeforeClose(ctx context.Context) bool {
	if a.trayMenuRef != nil {
		a.trayMenuRef.SetStatusText("状态: 后台运行")
	}
	return false // 返回 false 表示允许关闭 (隐藏) 窗口
}
