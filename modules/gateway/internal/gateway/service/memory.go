package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

const memoryContextLimit = 4000

type MemoryService struct {
	repos repository.Set
}

type MemoryDTO struct {
	ID              string         `json:"id"`
	Scope           string         `json:"scope"`
	Kind            string         `json:"kind"`
	Status          string         `json:"status"`
	Title           string         `json:"title"`
	Content         string         `json:"content"`
	Confidence      string         `json:"confidence"`
	WorkspaceRoot   string         `json:"workspace_root,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	RunID           string         `json:"run_id,omitempty"`
	SourceEventID   string         `json:"source_event_id,omitempty"`
	SourceMessageID string         `json:"source_message_id,omitempty"`
	Source          string         `json:"source"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       *time.Time     `json:"deleted_at,omitempty"`
}

type MemoryListRequest struct {
	Scope         string
	WorkspaceRoot string
	SessionID     string
	Status        string
	Limit         int
}

type MemoryCreateRequest struct {
	Scope           string         `json:"scope"`
	Kind            string         `json:"kind"`
	Status          string         `json:"status"`
	Title           string         `json:"title"`
	Content         string         `json:"content"`
	Confidence      string         `json:"confidence"`
	WorkspaceRoot   string         `json:"workspace_root"`
	SessionID       string         `json:"session_id"`
	RunID           string         `json:"run_id"`
	SourceEventID   string         `json:"source_event_id"`
	SourceMessageID string         `json:"source_message_id"`
	Source          string         `json:"source"`
	Metadata        map[string]any `json:"metadata"`
}

type MemoryUpdateRequest struct {
	Scope      *string        `json:"scope"`
	Kind       *string        `json:"kind"`
	Status     *string        `json:"status"`
	Title      *string        `json:"title"`
	Content    *string        `json:"content"`
	Confidence *string        `json:"confidence"`
	Metadata   map[string]any `json:"metadata"`
}

type MemoryPreviewRunRequest struct {
	SessionID     string `json:"session_id"`
	WorkspaceRoot string `json:"workspace_root"`
	Input         string `json:"input"`
}

type MemoryPreviewRunResult struct {
	Items   []MemoryDTO `json:"items"`
	Context string      `json:"context"`
}

func NewMemoryService(repos repository.Set) MemoryService {
	return MemoryService{repos: repos}
}

func (s MemoryService) List(req MemoryListRequest) ([]MemoryDTO, error) {
	scope, err := normalizeMemoryOptional(req.Scope, validMemoryScopes(), "scope")
	if err != nil {
		return nil, err
	}
	status, err := normalizeMemoryOptional(req.Status, append(validMemoryStatuses(), "all"), "status")
	if err != nil {
		return nil, err
	}
	rows, err := s.repos.Memory.List(repository.MemoryListFilter{
		Scope:         scope,
		WorkspaceRoot: strings.TrimSpace(req.WorkspaceRoot),
		SessionID:     strings.TrimSpace(req.SessionID),
		Status:        status,
		Limit:         req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return memoryDTOs(rows), nil
}

func (s MemoryService) Create(req MemoryCreateRequest) (MemoryDTO, error) {
	row, err := s.normalizeCreate(req)
	if err != nil {
		return MemoryDTO{}, err
	}
	created, err := s.repos.Memory.Create(row)
	if err != nil {
		return MemoryDTO{}, err
	}
	return memoryDTO(created), nil
}

func (s MemoryService) Update(id string, req MemoryUpdateRequest) (MemoryDTO, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryDTO{}, fmt.Errorf("memory id is required")
	}
	current, err := s.repos.Memory.Get(id)
	if err != nil {
		return MemoryDTO{}, err
	}
	update, err := s.normalizeUpdate(current, req)
	if err != nil {
		return MemoryDTO{}, err
	}
	updated, err := s.repos.Memory.Update(update)
	if err != nil {
		return MemoryDTO{}, err
	}
	return memoryDTO(updated), nil
}

func (s MemoryService) Delete(id string) (MemoryDTO, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryDTO{}, fmt.Errorf("memory id is required")
	}
	row, err := s.repos.Memory.Delete(id)
	if err != nil {
		return MemoryDTO{}, err
	}
	return memoryDTO(row), nil
}

func (s MemoryService) ExecuteRuntimeTool(params methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
	if strings.TrimSpace(params.RunID) == "" {
		return methods.MemoryToolExecuteResult{}, fmt.Errorf("run_id is required")
	}
	if strings.TrimSpace(params.ToolCallID) == "" {
		return methods.MemoryToolExecuteResult{}, fmt.Errorf("tool_call_id is required")
	}
	switch strings.TrimSpace(params.ToolName) {
	case "memory.list":
		return s.executeRuntimeMemoryList(params)
	case "memory.create":
		return s.executeRuntimeMemoryCreate(params)
	case "memory.update":
		return s.executeRuntimeMemoryUpdate(params)
	case "memory.delete":
		return s.executeRuntimeMemoryDelete(params)
	default:
		return methods.MemoryToolExecuteResult{}, fmt.Errorf("unsupported memory tool %q", params.ToolName)
	}
}

func (s MemoryService) PreviewRun(req MemoryPreviewRunRequest) (MemoryPreviewRunResult, error) {
	rows, err := s.repos.Memory.SelectForRun(strings.TrimSpace(req.SessionID), strings.TrimSpace(req.WorkspaceRoot), 10)
	if err != nil {
		return MemoryPreviewRunResult{}, err
	}
	items := make([]MemoryDTO, 0, len(rows))
	lines := make([]string, 0, len(rows))
	total := len("Memory:\n")
	for _, row := range rows {
		line := formatMemoryContextLine(row)
		if total+len(line)+1 > memoryContextLimit {
			break
		}
		items = append(items, memoryDTO(row))
		lines = append(lines, line)
		total += len(line) + 1
	}
	context := ""
	if len(lines) > 0 {
		context = "Memory:\n" + strings.Join(lines, "\n")
	}
	return MemoryPreviewRunResult{Items: items, Context: context}, nil
}

func (s MemoryService) executeRuntimeMemoryList(params methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
	scope, err := runtimeMemoryScope(params.Arguments, false)
	if err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	status := strings.ToLower(strings.TrimSpace(stringArgFromMap(params.Arguments, "status")))
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "disabled" {
		return methods.MemoryToolExecuteResult{}, fmt.Errorf("memory.list supports only active or disabled status")
	}
	limit := intArgFromMap(params.Arguments, "limit", 20)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var items []MemoryDTO
	if scope == "" || scope == "session" {
		sessionItems, err := s.List(MemoryListRequest{
			Scope:     "session",
			SessionID: strings.TrimSpace(params.SessionID),
			Status:    status,
			Limit:     limit,
		})
		if err != nil {
			return methods.MemoryToolExecuteResult{}, err
		}
		items = append(items, sessionItems...)
	}
	if scope == "" || scope == "project" {
		projectItems, err := s.List(MemoryListRequest{
			Scope:         "project",
			WorkspaceRoot: strings.TrimSpace(params.WorkspaceRoot),
			Status:        status,
			Limit:         limit,
		})
		if err != nil {
			return methods.MemoryToolExecuteResult{}, err
		}
		items = append(items, projectItems...)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	resultItems := memoryToolItems(items)
	return methods.MemoryToolExecuteResult{
		Status: "completed",
		Output: runtimeMemoryOutput("memory.list", resultItems),
		Items:  resultItems,
	}, nil
}

func (s MemoryService) executeRuntimeMemoryCreate(params methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
	scope, err := runtimeMemoryScope(params.Arguments, true)
	if err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	req := MemoryCreateRequest{
		Scope:         scope,
		Kind:          stringArgFromMap(params.Arguments, "kind"),
		Status:        "active",
		Title:         stringArgFromMap(params.Arguments, "title"),
		Content:       stringArgFromMap(params.Arguments, "content"),
		Confidence:    stringArgFromMap(params.Arguments, "confidence"),
		WorkspaceRoot: strings.TrimSpace(params.WorkspaceRoot),
		SessionID:     strings.TrimSpace(params.SessionID),
		RunID:         strings.TrimSpace(params.RunID),
		Source:        "agent",
		Metadata: map[string]any{
			"tool_call_id": strings.TrimSpace(params.ToolCallID),
		},
	}
	if rawStatus := strings.ToLower(strings.TrimSpace(stringArgFromMap(params.Arguments, "status"))); rawStatus == "disabled" {
		req.Status = "disabled"
	} else if rawStatus != "" && rawStatus != "active" {
		return methods.MemoryToolExecuteResult{}, fmt.Errorf("memory.create supports only active or disabled status")
	}
	created, err := s.Create(req)
	if err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	item := memoryToolItem(created)
	return methods.MemoryToolExecuteResult{
		Status:   "completed",
		Output:   runtimeMemoryOutput("memory.create", []methods.MemoryToolItem{item}),
		RecordID: created.ID,
		Items:    []methods.MemoryToolItem{item},
	}, nil
}

func (s MemoryService) executeRuntimeMemoryUpdate(params methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
	id := strings.TrimSpace(stringArgFromMap(params.Arguments, "id"))
	if id == "" {
		return methods.MemoryToolExecuteResult{}, fmt.Errorf("id is required")
	}
	current, err := s.repos.Memory.Get(id)
	if err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	if err := validateRuntimeMemoryOwnership(current, params); err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	if _, ok := params.Arguments["scope"]; ok {
		return methods.MemoryToolExecuteResult{}, fmt.Errorf("memory.update cannot change scope")
	}
	update := MemoryUpdateRequest{Metadata: map[string]any{
		"tool_call_id": strings.TrimSpace(params.ToolCallID),
	}}
	if value, ok := stringPointerArg(params.Arguments, "kind"); ok {
		update.Kind = value
	}
	if value, ok := stringPointerArg(params.Arguments, "status"); ok {
		normalized := strings.ToLower(strings.TrimSpace(*value))
		if normalized != "active" && normalized != "disabled" {
			return methods.MemoryToolExecuteResult{}, fmt.Errorf("memory.update supports only active or disabled status")
		}
		update.Status = &normalized
	}
	if value, ok := stringPointerArg(params.Arguments, "title"); ok {
		update.Title = value
	}
	if value, ok := stringPointerArg(params.Arguments, "content"); ok {
		update.Content = value
	}
	if value, ok := stringPointerArg(params.Arguments, "confidence"); ok {
		update.Confidence = value
	}
	updated, err := s.Update(id, update)
	if err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	item := memoryToolItem(updated)
	return methods.MemoryToolExecuteResult{
		Status:   "completed",
		Output:   runtimeMemoryOutput("memory.update", []methods.MemoryToolItem{item}),
		RecordID: updated.ID,
		Items:    []methods.MemoryToolItem{item},
	}, nil
}

func (s MemoryService) executeRuntimeMemoryDelete(params methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
	id := strings.TrimSpace(stringArgFromMap(params.Arguments, "id"))
	if id == "" {
		return methods.MemoryToolExecuteResult{}, fmt.Errorf("id is required")
	}
	current, err := s.repos.Memory.Get(id)
	if err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	if err := validateRuntimeMemoryOwnership(current, params); err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	deleted, err := s.Delete(id)
	if err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	item := memoryToolItem(deleted)
	return methods.MemoryToolExecuteResult{
		Status:   "completed",
		Output:   runtimeMemoryOutput("memory.delete", []methods.MemoryToolItem{item}),
		RecordID: deleted.ID,
		Items:    []methods.MemoryToolItem{item},
	}, nil
}

func (s MemoryService) normalizeCreate(req MemoryCreateRequest) (model.MemoryRecord, error) {
	scope, err := normalizeMemoryRequired(req.Scope, validMemoryScopes(), "scope")
	if err != nil {
		return model.MemoryRecord{}, err
	}
	kind, err := normalizeMemoryDefault(req.Kind, "fact", validMemoryKinds(), "kind")
	if err != nil {
		return model.MemoryRecord{}, err
	}
	status, err := normalizeMemoryDefault(req.Status, "active", validMemoryStatuses(), "status")
	if err != nil {
		return model.MemoryRecord{}, err
	}
	confidence, err := normalizeMemoryDefault(req.Confidence, "medium", validMemoryConfidences(), "confidence")
	if err != nil {
		return model.MemoryRecord{}, err
	}
	source, err := normalizeMemoryDefault(req.Source, "user", validMemorySources(), "source")
	if err != nil {
		return model.MemoryRecord{}, err
	}
	title := strings.TrimSpace(req.Title)
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return model.MemoryRecord{}, fmt.Errorf("content is required")
	}
	if title == "" {
		title = defaultMemoryTitle(content)
	}
	row := model.MemoryRecord{
		Scope:           scope,
		Kind:            kind,
		Status:          status,
		Title:           title,
		Content:         content,
		Confidence:      confidence,
		WorkspaceRoot:   strings.TrimSpace(req.WorkspaceRoot),
		SessionID:       strings.TrimSpace(req.SessionID),
		RunID:           strings.TrimSpace(req.RunID),
		SourceEventID:   strings.TrimSpace(req.SourceEventID),
		SourceMessageID: strings.TrimSpace(req.SourceMessageID),
		Source:          source,
	}
	metadataJSON, err := marshalMetadata(req.Metadata)
	if err != nil {
		return model.MemoryRecord{}, err
	}
	row.MetadataJSON = metadataJSON
	if err := validateMemoryTrace(row); err != nil {
		return model.MemoryRecord{}, err
	}
	return row, nil
}

func (s MemoryService) normalizeUpdate(current model.MemoryRecord, req MemoryUpdateRequest) (repository.MemoryUpdate, error) {
	update := repository.MemoryUpdate{ID: current.ID}
	next := current
	if req.Scope != nil {
		value, err := normalizeMemoryRequired(*req.Scope, validMemoryScopes(), "scope")
		if err != nil {
			return repository.MemoryUpdate{}, err
		}
		update.Scope = &value
		next.Scope = value
	}
	if req.Kind != nil {
		value, err := normalizeMemoryRequired(*req.Kind, validMemoryKinds(), "kind")
		if err != nil {
			return repository.MemoryUpdate{}, err
		}
		update.Kind = &value
	}
	if req.Status != nil {
		value, err := normalizeMemoryRequired(*req.Status, validMemoryStatuses(), "status")
		if err != nil {
			return repository.MemoryUpdate{}, err
		}
		update.Status = &value
		next.Status = value
	}
	if req.Title != nil {
		value := strings.TrimSpace(*req.Title)
		update.Title = &value
	}
	if req.Content != nil {
		value := strings.TrimSpace(*req.Content)
		if value == "" {
			return repository.MemoryUpdate{}, fmt.Errorf("content is required")
		}
		update.Content = &value
		next.Content = value
	}
	if req.Confidence != nil {
		value, err := normalizeMemoryRequired(*req.Confidence, validMemoryConfidences(), "confidence")
		if err != nil {
			return repository.MemoryUpdate{}, err
		}
		update.Confidence = &value
	}
	if req.Metadata != nil {
		value, err := marshalMetadata(req.Metadata)
		if err != nil {
			return repository.MemoryUpdate{}, err
		}
		update.MetadataJSON = &value
	}
	if err := validateMemoryTrace(next); err != nil {
		return repository.MemoryUpdate{}, err
	}
	return update, nil
}

func validateMemoryTrace(row model.MemoryRecord) error {
	switch row.Scope {
	case "project":
		if strings.TrimSpace(row.WorkspaceRoot) == "" {
			return fmt.Errorf("workspace_root is required for project memory")
		}
	case "session":
		if strings.TrimSpace(row.SessionID) == "" {
			return fmt.Errorf("session_id is required for session memory")
		}
	}
	if row.Source == "agent" && row.RunID == "" && row.SourceEventID == "" && row.SourceMessageID == "" {
		return fmt.Errorf("agent memory requires run_id, source_event_id, or source_message_id")
	}
	return nil
}

func runtimeMemoryScope(args map[string]any, required bool) (string, error) {
	scope := strings.ToLower(strings.TrimSpace(stringArgFromMap(args, "scope")))
	if scope == "" && !required {
		return "", nil
	}
	if scope != "project" && scope != "session" {
		if required {
			return "", fmt.Errorf("scope must be project or session")
		}
		return "", fmt.Errorf("memory tool scope must be project or session")
	}
	return scope, nil
}

func validateRuntimeMemoryOwnership(row model.MemoryRecord, params methods.MemoryToolExecuteParams) error {
	switch row.Scope {
	case "session":
		if strings.TrimSpace(row.SessionID) == "" || row.SessionID != strings.TrimSpace(params.SessionID) {
			return fmt.Errorf("memory record is outside the current session")
		}
	case "project":
		if strings.TrimSpace(row.WorkspaceRoot) == "" || row.WorkspaceRoot != strings.TrimSpace(params.WorkspaceRoot) {
			return fmt.Errorf("memory record is outside the current workspace")
		}
	default:
		return fmt.Errorf("memory tool cannot access %s scope", row.Scope)
	}
	if row.Status == "deleted" || row.DeletedAt != nil {
		return fmt.Errorf("memory record is deleted")
	}
	return nil
}

func memoryToolItems(items []MemoryDTO) []methods.MemoryToolItem {
	result := make([]methods.MemoryToolItem, 0, len(items))
	for _, item := range items {
		result = append(result, memoryToolItem(item))
	}
	return result
}

func memoryToolItem(item MemoryDTO) methods.MemoryToolItem {
	return methods.MemoryToolItem{
		ID:         item.ID,
		Scope:      item.Scope,
		Kind:       item.Kind,
		Status:     item.Status,
		Title:      item.Title,
		Content:    item.Content,
		Confidence: item.Confidence,
	}
}

func runtimeMemoryOutput(action string, items []methods.MemoryToolItem) string {
	raw, err := json.Marshal(map[string]any{
		"action": action,
		"items":  items,
	})
	if err != nil {
		return fmt.Sprintf("%s completed", action)
	}
	return string(raw)
}

func stringArgFromMap(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return value
}

func intArgFromMap(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func stringPointerArg(args map[string]any, key string) (*string, bool) {
	if args == nil {
		return nil, false
	}
	value, ok := args[key]
	if !ok {
		return nil, false
	}
	text, _ := value.(string)
	return &text, true
}

func memoryDTOs(rows []model.MemoryRecord) []MemoryDTO {
	items := make([]MemoryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, memoryDTO(row))
	}
	return items
}

func memoryDTO(row model.MemoryRecord) MemoryDTO {
	return MemoryDTO{
		ID:              row.ID,
		Scope:           row.Scope,
		Kind:            row.Kind,
		Status:          row.Status,
		Title:           row.Title,
		Content:         row.Content,
		Confidence:      row.Confidence,
		WorkspaceRoot:   row.WorkspaceRoot,
		SessionID:       row.SessionID,
		RunID:           row.RunID,
		SourceEventID:   row.SourceEventID,
		SourceMessageID: row.SourceMessageID,
		Source:          row.Source,
		Metadata:        unmarshalMetadata(row.MetadataJSON),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		DeletedAt:       row.DeletedAt,
	}
}

func normalizeMemoryRequired(value string, allowed []string, field string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	for _, item := range allowed {
		if normalized == item {
			return normalized, nil
		}
	}
	return "", fmt.Errorf("unsupported %s %q", field, value)
}

func normalizeMemoryDefault(value string, fallback string, allowed []string, field string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		normalized = fallback
	}
	return normalizeMemoryRequired(normalized, allowed, field)
}

func normalizeMemoryOptional(value string, allowed []string, field string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", nil
	}
	return normalizeMemoryRequired(normalized, allowed, field)
}

func validMemoryScopes() []string {
	return []string{"project", "session", "user", "system"}
}

func validMemoryKinds() []string {
	return []string{"fact", "preference", "decision", "task", "summary", "warning"}
}

func validMemoryStatuses() []string {
	return []string{"active", "disabled", "deleted"}
}

func validMemoryConfidences() []string {
	return []string{"low", "medium", "high"}
}

func validMemorySources() []string {
	return []string{"user", "agent", "compaction", "import", "system"}
}

func marshalMetadata(metadata map[string]any) (string, error) {
	if len(metadata) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("metadata must be valid JSON")
	}
	return string(raw), nil
}

func unmarshalMetadata(value string) map[string]any {
	if value == "" {
		return nil
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(value), &metadata); err != nil {
		return nil
	}
	return metadata
}

func defaultMemoryTitle(content string) string {
	runes := []rune(content)
	if len(runes) > 48 {
		return string(runes[:45]) + "..."
	}
	return content
}

func formatMemoryContextLine(row model.MemoryRecord) string {
	title := strings.TrimSpace(row.Title)
	if title == "" {
		title = defaultMemoryTitle(row.Content)
	}
	return fmt.Sprintf("- [%s/%s] %s: %s", row.Scope, row.Kind, title, strings.TrimSpace(row.Content))
}
