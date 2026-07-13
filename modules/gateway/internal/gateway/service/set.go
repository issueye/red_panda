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
	Todo       TodoService
	Tool       ToolService
	Permission PermissionService
	Provider   ProviderProfileService
	MCPServers MCPServerConfigService
	Skills     SkillService
	Agents     AgentDefinitionService
	Goal       GoalService
	Context    ContextService
}

type AppService struct {
	Version string
}

func NewSet(opts Options) Set {
	return Set{
		App:        AppService{Version: opts.Version},
		Run:        NewRunService(opts.Repos, opts.Hub, opts.RuntimeClient),
		Workspace:  NewWorkspaceService(opts.Repos),
		Session:    NewSessionService(opts.Repos, opts.RuntimeClient),
		Memory:     NewMemoryService(opts.Repos),
		Todo:       NewTodoService(opts.Repos),
		Tool:       NewToolService(opts.Repos),
		Permission: NewPermissionService(opts.Repos),
		Provider:   NewProviderProfileService(opts.Repos),
		MCPServers: NewMCPServerConfigService(opts.Repos, opts.RuntimeClient),
		Skills:     NewSkillService(opts.RuntimeClient),
		Agents:     NewAgentDefinitionService(opts.Repos),
		Goal:       NewGoalService(opts.Repos),
		Context:    NewContextService(opts.Repos),
	}
}
