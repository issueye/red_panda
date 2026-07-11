package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	protows "redpanda/protocol/ws"
)

type RunService struct {
	repos   repository.Set
	hub     *eventhub.Hub
	runtime *runtimeclient.Client
}

type StartRunResult struct {
	RunID        string `json:"run_id"`
	SessionID    string `json:"session_id"`
	Accepted     bool   `json:"accepted"`
	Subscribed   bool   `json:"subscribed"`
	RuntimeMode  string `json:"runtime_mode"`
	RootSequence uint64 `json:"root_seq"`
}

type RunRecordDTO struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	WorkspaceRoot string     `json:"workspace_root,omitempty"`
	RuntimeMode   string     `json:"runtime_mode"`
	Status        string     `json:"status"`
	Input         string     `json:"input,omitempty"`
	LastEventType string     `json:"last_event_type,omitempty"`
	LastRootSeq   uint64     `json:"last_root_seq"`
	MessageCount  int        `json:"message_count"`
	ToolCount     int        `json:"tool_count"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type RunEventDTO struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	RootRunID   string         `json:"root_run_id"`
	RunID       string         `json:"run_id"`
	ParentRunID string         `json:"parent_run_id,omitempty"`
	SessionID   string         `json:"session_id"`
	RootSeq     uint64         `json:"root_seq"`
	AgentSeq    uint64         `json:"agent_seq"`
	AgentID     string         `json:"agent_id"`
	AgentRole   string         `json:"agent_role"`
	AgentName   string         `json:"agent_name,omitempty"`
	StreamKind  string         `json:"stream_kind,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

func NewRunService(repos repository.Set, hub *eventhub.Hub, runtime *runtimeclient.Client) RunService {
	return RunService{repos: repos, hub: hub, runtime: runtime}
}

func (r RunService) RuntimeStatus() map[string]any {
	if r.runtime == nil {
		return map[string]any{
			"available": false,
			"mode":      "single_core",
			"reason":    "runtime client not configured",
		}
	}
	return r.runtime.Status()
}

func (r RunService) Get(runID string) (RunRecordDTO, error) {
	row, err := r.repos.Runs.Get(runID)
	if err != nil {
		return RunRecordDTO{}, err
	}
	return runRecordDTO(row), nil
}

func (r RunService) ListBySession(sessionID string, limit int) ([]RunRecordDTO, error) {
	rows, err := r.repos.Runs.ListBySession(sessionID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RunRecordDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, runRecordDTO(row))
	}
	return items, nil
}

func (r RunService) Events(rootRunID string, afterSeq uint64, limit int) ([]RunEventDTO, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	items, err := r.repos.RunEvents.ListAfter(rootRunID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	dtos := make([]RunEventDTO, 0, len(items))
	for _, event := range items {
		dtos = append(dtos, runEventDTO(event))
	}
	return dtos, nil
}

func (r RunService) Start(ctx context.Context, payload protows.RunStartPayload) (StartRunResult, error) {
	if r.runtime == nil {
		return StartRunResult{}, fmt.Errorf("runtime client not configured")
	}

	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())
	sessionID := payload.SessionID
	if sessionID == "" {
		sessionID = "session_" + runID
	}
	workspaceRoot := stringOption(payload.Options, "working_dir")
	session, err := r.repos.Sessions.Ensure(sessionID, sessionID, workspaceRoot)
	if err != nil {
		return StartRunResult{}, err
	}
	history, err := r.repos.Messages.ListLatestConversation(session.ID, 200)
	if err != nil {
		return StartRunResult{}, err
	}
	conversation := make([]methods.Message, 0, len(history))
	for _, row := range history {
		message, err := messageDTO(row)
		if err != nil {
			return StartRunResult{}, err
		}
		conversation = append(conversation, methods.Message{
			ID:        message.ID,
			Role:      message.Role,
			Content:   message.Content,
			CreatedAt: message.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	if _, err := r.repos.Messages.Add(session.ID, "user", stringInput(payload.Input, "text"), runID); err != nil {
		return StartRunResult{}, err
	}

	params := methods.ReplyParams{
		RunID: runID,
		Session: methods.ReplySession{
			ID:           session.ID,
			Name:         session.Name,
			WorkingDir:   session.WorkspaceRoot,
			Conversation: conversation,
		},
		Input: methods.ReplyInput{
			Text: stringInput(payload.Input, "text"),
		},
		Options: methods.ReplyOptions{
			ProviderProfileID:   stringOption(payload.Options, "provider_profile_id"),
			Model:               stringOption(payload.Options, "model"),
			PermissionMode:      stringOption(payload.Options, "permission_mode"),
			ToolPolicy:          stringOption(payload.Options, "tool_policy"),
			ToolAllowlist:       stringSliceOption(payload.Options, "tool_allowlist"),
			ToolDenylist:        stringSliceOption(payload.Options, "tool_denylist"),
			EmitToolEvents:      true,
			RequirePermission:   boolOption(payload.Options, "require_permission"),
			SpawnSubAgents:      boolOption(payload.Options, "spawn_subagents"),
			SubAgentBackend:     stringOption(payload.Options, "subagent_backend"),
			WebSearchMaxResults: intOption(payload.Options, "web_search_max_results"),
			WebFetchMaxBytes:    intOption(payload.Options, "web_fetch_max_bytes"),
		},
	}
	if err := r.applyProviderProfile(&params); err != nil {
		return StartRunResult{}, err
	}
	if err := r.applyMemoryContext(&params); err != nil {
		return StartRunResult{}, err
	}
	runtimeMode := normalizedRuntimeMode(stringOption(payload.Options, "runtime_mode"))

	accepted, err := r.runtime.ReplyWithMode(ctx, runtimeMode, params)
	if err != nil {
		return StartRunResult{}, err
	}
	_ = r.repos.Runs.Start(model.RunRecord{
		ID:            accepted.RunID,
		SessionID:     session.ID,
		WorkspaceRoot: session.WorkspaceRoot,
		RuntimeMode:   runtimeMode,
		Status:        "running",
		Input:         params.Input.Text,
		StartedAt:     time.Now().UTC(),
	})
	_ = r.repos.Sessions.Touch(session.ID)
	return StartRunResult{
		RunID:       accepted.RunID,
		SessionID:   session.ID,
		Accepted:    accepted.Accepted,
		Subscribed:  payload.Subscribe,
		RuntimeMode: runtimeMode,
	}, nil
}

func (r RunService) applyMemoryContext(params *methods.ReplyParams) error {
	preview, err := NewMemoryService(r.repos).PreviewRun(MemoryPreviewRunRequest{
		SessionID:     params.Session.ID,
		WorkspaceRoot: params.Session.WorkingDir,
		Input:         params.Input.Text,
	})
	if err != nil {
		return err
	}
	if len(preview.Items) == 0 || preview.Context == "" {
		return nil
	}
	items := make([]methods.MemoryItem, 0, len(preview.Items))
	for _, item := range preview.Items {
		items = append(items, methods.MemoryItem{
			ID:      item.ID,
			Scope:   item.Scope,
			Kind:    item.Kind,
			Title:   item.Title,
			Content: item.Content,
		})
	}
	params.Options.MemoryContext = &methods.MemoryContext{
		Items:   items,
		Context: preview.Context,
	}
	return nil
}

func (r RunService) applyProviderProfile(params *methods.ReplyParams) error {
	profileID := params.Options.ProviderProfileID
	if profileID == "" {
		return nil
	}
	profile, err := r.repos.Providers.Get(profileID)
	if err != nil {
		return fmt.Errorf("provider profile %s not found: %w", profileID, err)
	}
	if !profile.Active {
		return fmt.Errorf("provider profile %s is inactive", profileID)
	}
	params.Options.ProviderName = profile.Provider
	params.Options.ProviderBaseURL = profile.BaseURL
	params.Options.ProviderAPIKey = profile.APIKeySecret
	if params.Options.Model == "" {
		params.Options.Model = profile.Model
	}
	return nil
}

func (r RunService) Cancel(ctx context.Context, runID string, reason string) error {
	if r.runtime == nil {
		return fmt.Errorf("runtime client not configured")
	}
	return r.runtime.Cancel(ctx, methods.CancelParams{RunID: runID, Reason: reason})
}

func (r RunService) SubAgents(ctx context.Context, params methods.SubAgentsParams) (methods.SubAgentsResult, error) {
	if r.runtime == nil {
		return methods.SubAgentsResult{}, fmt.Errorf("runtime client not configured")
	}
	return r.runtime.SubAgents(ctx, params)
}

func (r RunService) CancelSubAgent(ctx context.Context, params methods.SubAgentCancelParams) (methods.SubAgentCancelResult, error) {
	if r.runtime == nil {
		return methods.SubAgentCancelResult{}, fmt.Errorf("runtime client not configured")
	}
	return r.runtime.CancelSubAgent(ctx, params)
}

func (r RunService) ResolvePermission(ctx context.Context, params permission.ResolveParams) (permission.ResolveResult, error) {
	if r.runtime == nil {
		return permission.ResolveResult{}, fmt.Errorf("runtime client not configured")
	}
	result, err := r.runtime.ResolvePermission(ctx, params)
	if err != nil {
		return permission.ResolveResult{}, err
	}
	_ = r.repos.Permissions.Resolve(params)
	return result, nil
}

func (r RunService) Subscribe(rootRunID string) (<-chan events.Envelope, func()) {
	return r.hub.Subscribe(rootRunID)
}

func (r RunService) Replay(rootRunID string, afterSeq uint64) ([]events.Envelope, error) {
	return r.repos.RunEvents.ListAfter(rootRunID, afterSeq, 500)
}

func (r RunService) HandleRuntimeEvent(event events.Envelope) {
	if event.EventID == "" || event.RootRunID == "" {
		return
	}
	_ = r.repos.Runs.ProjectEvent(event)
	if event.Type == events.EventPermissionRequest {
		_ = r.repos.Permissions.ProjectRequired(event)
	}
	if isToolEvent(event.Type) {
		_ = r.repos.ToolCalls.Project(event)
		_ = r.repos.Runs.RefreshToolCount(event.RootRunID)
	}
	if event.Type == events.EventFinish {
		_ = r.repos.Permissions.ClosePendingByRun(event.RootRunID, "closed", "run finished")
	}
	_ = r.repos.RunEvents.Save(event)
	if event.Type == events.EventMessageDelta || event.Type == events.EventReasoningDelta {
		if delta, ok := event.Payload["delta"].(string); ok && delta != "" {
			role := "assistant"
			if event.Agent.Role == events.AgentRoleSubAgent {
				role = "subagent"
			}
			_, _ = r.repos.Messages.AddOrAppend(event.SessionID, role, delta, event.RootRunID)
			_ = r.repos.Sessions.Touch(event.SessionID)
		}
	}
	r.hub.Publish(event)
}

func isToolEvent(typ events.EventType) bool {
	return typ == events.EventToolStarted ||
		typ == events.EventToolOutput ||
		typ == events.EventToolFinished ||
		typ == events.EventToolFailed
}

func stringInput(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, _ := input[key].(string)
	return value
}

func stringOption(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	switch value := options[key].(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func boolOption(options map[string]any, key string) bool {
	if options == nil {
		return false
	}
	value, _ := options[key].(bool)
	return value
}

func intOption(options map[string]any, key string) int {
	if options == nil {
		return 0
	}
	switch value := options[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return 0
}

func stringSliceOption(options map[string]any, key string) []string {
	if options == nil {
		return nil
	}
	raw, ok := options[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return value
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

func normalizedRuntimeMode(mode string) string {
	if mode == "per_run_process" {
		return "per_run_process"
	}
	return "single_core"
}

func runRecordDTO(row model.RunRecord) RunRecordDTO {
	return RunRecordDTO{
		ID:            row.ID,
		SessionID:     row.SessionID,
		WorkspaceRoot: row.WorkspaceRoot,
		RuntimeMode:   row.RuntimeMode,
		Status:        row.Status,
		Input:         row.Input,
		LastEventType: row.LastEventType,
		LastRootSeq:   row.LastRootSeq,
		MessageCount:  row.MessageCount,
		ToolCount:     row.ToolCount,
		Error:         row.Error,
		StartedAt:     row.StartedAt,
		FinishedAt:    row.FinishedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func runEventDTO(event events.Envelope) RunEventDTO {
	streamKind := ""
	if event.Stream != nil {
		streamKind = string(event.Stream.Kind)
	}
	return RunEventDTO{
		ID:          event.EventID,
		Type:        string(event.Type),
		RootRunID:   event.RootRunID,
		RunID:       event.RunID,
		ParentRunID: event.ParentRunID,
		SessionID:   event.SessionID,
		RootSeq:     event.RootSeq,
		AgentSeq:    event.AgentSeq,
		AgentID:     event.Agent.AgentID,
		AgentRole:   string(event.Agent.Role),
		AgentName:   event.Agent.Name,
		StreamKind:  streamKind,
		Payload:     event.Payload,
		CreatedAt:   event.CreatedAt,
	}
}
