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
	desktop := desktopapp.New("0.1.0")

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
		Width:            1280,
		Height:           820,
		MinWidth:         980,
		MinHeight:        640,
		BackgroundColour: application.NewRGB(247, 248, 251),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
