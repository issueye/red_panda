package app

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
