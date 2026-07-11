package app

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type BootstrapInfo struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	GatewayBase   string `json:"gateway_base"`
	WebSocketPath string `json:"websocket_path"`
}

type App struct {
	info BootstrapInfo
}

func New(version string) *App {
	return &App{
		info: BootstrapInfo{
			Name:          "red_panda",
			Version:       version,
			GatewayBase:   "http://127.0.0.1:17888",
			WebSocketPath: "/api/v1/ws",
		},
	}
}

func (a *App) Bootstrap() BootstrapInfo {
	return a.info
}

// SelectDirectory opens a native folder picker and returns the selected path.
// Empty string means the user cancelled.
func (a *App) SelectDirectory() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("desktop application is not ready")
	}
	return app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		SetTitle("选择工作区").
		PromptForSingleSelection()
}
