package main

import (
	_ "embed"

	"fyne.io/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// startTray 在独立 goroutine 中启动系统托盘，避免阻塞主流程。
func (a *App) startTray() {
	go func() {
		// systray.Run 内部会锁定自己的 OS 线程并运行消息循环
		systray.Run(a.onTrayReady, a.onTrayExit)
	}()
	// shutdown 需依据该标记决定是否调用 systray.Quit
	a.trayOn = true
}

// onTrayReady 托盘初始化：设置图标与菜单。
func (a *App) onTrayReady() {
	systray.SetTitle("LightMark")
	systray.SetTooltip("LightMark")
	if len(trayIcon) > 0 {
		systray.SetIcon(trayIcon)
	}
	show := systray.AddMenuItem("显示主窗口", "显示 LightMark 主窗口")
	quit := systray.AddMenuItem("退出", "退出 LightMark")

	go func() {
		for {
			select {
			case <-show.ClickedCh:
				a.showMainWindow()
			case <-quit.ClickedCh:
				runtime.Quit(a.ctx)
			}
		}
	}()
}

// onTrayExit 托盘退出回调，暂无额外清理。
func (a *App) onTrayExit() {}
