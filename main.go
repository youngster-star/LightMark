package main

import (
	"context"
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"light_mark/internal/singleinstance"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 远程桌面（向日葵/ToDesk 等）下 GPU 合成内容无法被捕获，强制软件合成
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--disable-gpu-compositing --disable-direct-composition --disable-gpu-rasterization")

	// 单实例约束：检测是否已有实例在运行。已有实例时唤出其主窗口后退出，
	// 避免重复打开多个 LightMark。
	first, single, err := singleinstance.Acquire()
	if err != nil {
		// 检测异常按正常启动降级处理，不阻断使用
		println("单实例检测失败，按正常流程启动:", err.Error())
	} else if !first {
		// 已有实例在运行：唤起主窗口并退出本进程
		singleinstance.NotifyExisting()
		return
	}

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err = wails.Run(&options.App{
		Title:  "LightMark",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:  &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		HideWindowOnClose: true,
		DragAndDrop:       &options.DragAndDrop{EnableFileDrop: true},
		Windows: &windows.Options{
			// 禁用 GPU 加速，避免远程桌面/虚拟显示驱动下 WebView2 白屏
			WebviewGpuIsDisabled: true,
			// 允许远程控制软件（如向日葵）注入的 DLL，避免渲染管线被阻断
			WebviewDisableRendererCodeIntegrity: true,
		},
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
			// 注册单实例激活回调：后续重复启动时唤出并聚焦主窗口
			if single != nil {
				_ = single.Wait(app.showMainWindow)
			}
		},
		OnShutdown: func(ctx context.Context) {
			app.shutdown(ctx)
			if single != nil {
				single.Release()
			}
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
