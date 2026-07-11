package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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

// OpenInExplorer reveals a file or directory in the system file manager.
// Directories open in-place; files are selected/revealed when the platform supports it.
func (a *App) OpenInExplorer(path string) error {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "windows":
		if info.IsDir() {
			return exec.Command("explorer.exe", abs).Start()
		}
		// /select, must be a single combined argument for explorer.exe.
		return exec.Command("explorer.exe", "/select,"+abs).Start()
	case "darwin":
		if info.IsDir() {
			return exec.Command("open", abs).Start()
		}
		return exec.Command("open", "-R", abs).Start()
	default:
		target := abs
		if !info.IsDir() {
			target = filepath.Dir(abs)
		}
		return exec.Command("xdg-open", target).Start()
	}
}
