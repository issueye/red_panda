package model

import "time"

type Session struct {
	ID              string `gorm:"primaryKey"`
	Name            string
	WorkspaceRoot   string
	Status          string
	ParentID        string
	Kind            string
	SourceSessionID string
	ForkPointSeq    uint64
	ForkPointRunID  string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

type Message struct {
	ID              string `gorm:"primaryKey"`
	SessionID       string `gorm:"index"`
	Role            string
	ContentJSON     string
	Seq             uint64
	RunID           string `gorm:"index"`
	SourceMessageID string
	MetadataJSON    string
	CreatedAt       time.Time
}

type SessionLineage struct {
	ID              string `gorm:"primaryKey"`
	SourceSessionID string `gorm:"index"`
	TargetSessionID string `gorm:"index"`
	Operation       string `gorm:"index"`
	ForkPointSeq    uint64
	ForkPointRunID  string
	SourceStartSeq  uint64
	SourceEndSeq    uint64
	MetadataJSON    string
	CreatedAt       time.Time
}

type SessionCompaction struct {
	ID               string `gorm:"primaryKey"`
	SourceSessionID  string `gorm:"index"`
	TargetSessionID  string `gorm:"index"`
	Status           string `gorm:"index"`
	SourceStartSeq   uint64
	SourceEndSeq     uint64
	SummaryMessageID string
	SummaryJSON      string
	Error            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type MemoryRecord struct {
	ID              string `gorm:"primaryKey"`
	Scope           string `gorm:"index"`
	Kind            string `gorm:"index"`
	Status          string `gorm:"index"`
	Title           string
	Content         string
	Confidence      string `gorm:"index"`
	WorkspaceRoot   string `gorm:"index"`
	SessionID       string `gorm:"index"`
	RunID           string `gorm:"index"`
	SourceEventID   string
	SourceMessageID string
	Source          string `gorm:"index"`
	MetadataJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

type RunEvent struct {
	ID          string `gorm:"primaryKey"`
	RootRunID   string `gorm:"uniqueIndex:idx_run_seq"`
	RootSeq     uint64 `gorm:"uniqueIndex:idx_run_seq"`
	Type        string
	PayloadJSON string
	CreatedAt   time.Time
}

type RunRecord struct {
	ID            string `gorm:"primaryKey"`
	SessionID     string `gorm:"index"`
	WorkspaceRoot string
	RuntimeMode   string
	Status        string `gorm:"index"`
	Input         string
	LastEventType string
	LastRootSeq   uint64
	MessageCount  int
	ToolCount     int
	Error         string
	StartedAt     time.Time
	FinishedAt    *time.Time
	UpdatedAt     time.Time
}

type ToolCall struct {
	ID            string `gorm:"primaryKey"`
	RootRunID     string `gorm:"index"`
	SessionID     string `gorm:"index"`
	AgentID       string
	AgentRole     string
	ToolName      string `gorm:"index"`
	DisplayName   string
	Risk          string
	Policy        string
	PolicyReason  string
	ArgumentsJSON string
	Status        string `gorm:"index"`
	Output        string
	Error         string
	ExitCode      int
	DurationMS    int64
	StartedSeq    uint64
	FinishedSeq   uint64
	StartedAt     time.Time
	FinishedAt    *time.Time
	UpdatedAt     time.Time
}

type Workspace struct {
	ID           string `gorm:"primaryKey"`
	Root         string `gorm:"uniqueIndex"`
	Name         string
	LastOpenedAt time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PermissionRequest struct {
	ID            string `gorm:"primaryKey"`
	RunID         string `gorm:"index"`
	SessionID     string `gorm:"index"`
	ToolCallID    string
	ToolName      string
	Risk          string
	Summary       string
	Detail        string
	ArgumentsJSON string
	Status        string `gorm:"index"`
	Decision      string
	Reason        string
	RootSeq       uint64
	CreatedAt     time.Time
	ResolvedAt    *time.Time
	UpdatedAt     time.Time
}

type ProviderProfile struct {
	ID           string `gorm:"primaryKey"`
	Name         string
	Provider     string `gorm:"index"`
	BaseURL      string
	Model        string
	APIKeySecret string
	IsDefault    bool `gorm:"index"`
	Active       bool `gorm:"index"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}
