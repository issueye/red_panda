package service

import (
	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/repository"
)

type Options struct {
	Version           string
	Repos             repository.Set
	Hub               *eventhub.Hub
	RuntimeClient     *runtimeclient.Client
	SessionArchiveDir string // JSONL archives for hard-deleted sessions (docs/49)
}

type Set struct {
	App            AppService
	Run            RunService
	Workspace      WorkspaceService
	Session        SessionService
	Memory         MemoryService
	Todo           TodoService
	Tool           ToolService
	Permission     PermissionService
	Provider       ProviderProfileService
	MCPServers     MCPServerConfigService
	Skills         SkillService
	WorkerProfiles WorkerProfileService
	Goal           GoalService
	Context        ContextService
	Schedule       ScheduleService
}

type AppService struct {
	Version string
}

func NewSet(opts Options) Set {
	run := NewRunService(opts.Repos, opts.Hub, opts.RuntimeClient)
	archiveDir := opts.SessionArchiveDir
	lifecycle := newSessionLifecycle(opts.Repos, opts.RuntimeClient, opts.Hub, archiveDir)
	return Set{
		App:            AppService{Version: opts.Version},
		Run:            run,
		Workspace:      NewWorkspaceService(opts.Repos, lifecycle),
		Session:        NewSessionService(opts.Repos, opts.RuntimeClient, opts.Hub, archiveDir),
		Memory:         NewMemoryService(opts.Repos),
		Todo:           NewTodoService(opts.Repos),
		Tool:           NewToolService(opts.Repos),
		Permission:     NewPermissionService(opts.Repos),
		Provider:       NewProviderProfileService(opts.Repos),
		MCPServers:     NewMCPServerConfigService(opts.Repos, opts.RuntimeClient),
		Skills:         NewSkillService(opts.RuntimeClient),
		WorkerProfiles: NewWorkerProfileService(opts.Repos),
		Goal:           NewGoalService(opts.Repos),
		Context:        NewContextService(opts.Repos),
		Schedule:       NewScheduleService(opts.Repos, opts.Hub, run),
	}
}
