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
	AttachmentsDir    string // binary asset storage root (docs/51); empty → DefaultAttachmentsDir(dsn)
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
	Schedule       ScheduleService
	Attachments    AttachmentService
}

type AppService struct {
	Version string
}

func NewSet(opts Options) Set {
	archiveDir := opts.SessionArchiveDir
	// Shared collaborators (single instance handed to multiple services).
	packer := NewSessionContextPacker(opts.Repos)
	purge := newPurgeService(opts.Repos, opts.RuntimeClient, opts.Hub, archiveDir)
	run := NewRunServiceWithPacker(opts.Repos, opts.Hub, opts.RuntimeClient, packer)
	workspace := NewWorkspaceService(opts.Repos, purge)
	attachments := NewAttachmentService(opts.Repos, opts.AttachmentsDir, workspace)
	run.AttachAttachmentService(&attachments)
	session := NewSessionServiceWithPacker(opts.Repos, opts.RuntimeClient, opts.Hub, archiveDir, packer)
	session.AttachAttachmentService(&attachments)
	return Set{
		App:            AppService{Version: opts.Version},
		Run:            run,
		Workspace:      workspace,
		Attachments:    attachments,
		// SessionService internally shares one sessionStore with its SessionCompactor.
		Session:        session,
		Memory:         NewMemoryService(opts.Repos),
		Todo:           NewTodoService(opts.Repos),
		Tool:           NewToolService(opts.Repos),
		Permission:     NewPermissionService(opts.Repos),
		Provider:       NewProviderProfileService(opts.Repos),
		MCPServers:     NewMCPServerConfigService(opts.Repos, opts.RuntimeClient),
		Skills:         NewSkillService(opts.RuntimeClient),
		WorkerProfiles: NewWorkerProfileService(opts.Repos),
		Schedule:       NewScheduleService(opts.Repos, opts.Hub, run),
	}
}
