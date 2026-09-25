package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 远程桌面（向日葵/ToDesk 等）下 GPU 合成内容无法被捕获，强制软件合成
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--disable-gpu-compositing --disable-direct-composition --disable-gpu-rasterization")

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
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
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
