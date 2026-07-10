package controller

import (
	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/service"
)

type Set struct {
	Health     HealthController
	App        AppController
	Agent      AgentController
	Run        RunController
	Workspace  WorkspaceController
	Session    SessionController
	Memory     MemoryController
	Tool       ToolController
	Permission PermissionController
	Provider   ProviderProfileController
	MCPServers MCPServerConfigController
	WebSocket  WebSocketController
}

func NewSet(services service.Set, hub *eventhub.Hub) Set {
	return Set{
		Health:     HealthController{Services: services},
		App:        AppController{Services: services},
		Agent:      AgentController{Services: services},
		Run:        RunController{Services: services},
		Workspace:  WorkspaceController{Services: services},
		Session:    SessionController{Services: services},
		Memory:     MemoryController{Services: services},
		Tool:       ToolController{Services: services},
		Permission: PermissionController{Services: services},
		Provider:   ProviderProfileController{Services: services},
		MCPServers: MCPServerConfigController{Services: services},
		WebSocket: WebSocketController{
			Services: services,
			Hub:      hub,
		},
	}
}
