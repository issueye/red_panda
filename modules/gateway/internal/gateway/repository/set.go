package repository

import "gorm.io/gorm"

type Set struct {
	DB          *gorm.DB
	Runs        RunRecordRepository
	RunEvents   RunEventRepository
	Workspaces  WorkspaceRepository
	Sessions    SessionRepository
	Lineage     SessionLineageRepository
	Compactions SessionCompactionRepository
	Memory      MemoryRepository
	Messages    MessageRepository
	ToolCalls   ToolCallRepository
	Permissions PermissionRequestRepository
	Providers   ProviderProfileRepository
}

func NewSet(db *gorm.DB) Set {
	return Set{
		DB:          db,
		Runs:        NewRunRecordRepository(db),
		RunEvents:   NewRunEventRepository(db),
		Workspaces:  NewWorkspaceRepository(db),
		Sessions:    NewSessionRepository(db),
		Lineage:     NewSessionLineageRepository(db),
		Compactions: NewSessionCompactionRepository(db),
		Memory:      NewMemoryRepository(db),
		Messages:    NewMessageRepository(db),
		ToolCalls:   NewToolCallRepository(db),
		Permissions: NewPermissionRequestRepository(db),
		Providers:   NewProviderProfileRepository(db),
	}
}
