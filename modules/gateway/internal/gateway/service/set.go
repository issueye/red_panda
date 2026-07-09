package service

import (
	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/repository"
)

type Options struct {
	Version       string
	Repos         repository.Set
	Hub           *eventhub.Hub
	RuntimeClient *runtimeclient.Client
}

type Set struct {
	App        AppService
	Run        RunService
	Workspace  WorkspaceService
	Session    SessionService
	Memory     MemoryService
	Tool       ToolService
	Permission PermissionService
	Provider   ProviderProfileService
}

type AppService struct {
	Version string
}

func NewSet(opts Options) Set {
	return Set{
		App:        AppService{Version: opts.Version},
		Run:        NewRunService(opts.Repos, opts.Hub, opts.RuntimeClient),
		Workspace:  NewWorkspaceService(opts.Repos),
		Session:    NewSessionService(opts.Repos),
		Memory:     NewMemoryService(opts.Repos),
		Tool:       NewToolService(opts.Repos),
		Permission: NewPermissionService(opts.Repos),
		Provider:   NewProviderProfileService(opts.Repos),
	}
}
