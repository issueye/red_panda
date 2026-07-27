package main

import (
	"embed"
	"log"

	desktopapp "redpanda/desktop/app"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	desktop := desktopapp.New("0.2.0")

	app := application.New(application.Options{
		Name:        "red_panda",
		Description: "Local AI Agent desktop shell",
		Services: []application.Service{
			application.NewService(desktop),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "red_panda",
		Width:            1400,
		Height:           900,
		MinWidth:         1400,
		MinHeight:        900,
		BackgroundColour: application.NewRGB(247, 248, 251),
		URL:              "/",
		// Custom global header (TopBar) replaces the native title bar.
		Frameless: true,
		Windows: application.WindowsWindow{
			// Keep Aero shadow / rounded corners while using a custom caption bar.
			DisableFramelessWindowDecorations: false,
			// Enable CSS --wails-draggable hit-testing for drag / caption regions.
			NonClientRegionSupport: true,
		},
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHidden,
			InvisibleTitleBarHeight: 48,
		},
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
