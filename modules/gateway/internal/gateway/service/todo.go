package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

const (
	todoContextLimit   = 3000
	todoMaxItems       = 50
	todoMaxContentRunes = 2000
	todoMaxLineRunes   = 200
)

type TodoService struct {
	repos repository.Set
}

type TodoDTO struct {
	ID               string     `json:"id"`
	ClientKey        string     `json:"client_key,omitempty"`
	SessionID        string     `json:"session_id"`
	Content          string     `json:"content"`
	Status           string     `json:"status"`
	SortOrder        int        `json:"sort_order"`
	Priority         string     `json:"priority,omitempty"`
	ActiveForm       string     `json:"active_form,omitempty"`
	SourceRunID      string     `json:"source_run_id,omitempty"`
	SourceToolCallID string     `json:"source_tool_call_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

type TodoListHTTPResult struct {
	Items     []TodoDTO `json:"items"`
	OpenCount int       `json:"open_count"`
}

func NewTodoService(repos repository.Set) TodoService {
	return TodoService{repos: repos}
}

func (s TodoService) ListBySession(sessionID string) (TodoListHTTPResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return TodoListHTTPResult{}, fmt.Errorf("session id is required")
	}
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		return TodoListHTTPResult{}, fmt.Errorf("session not found")
	}
	rows, err := s.repos.Todos.ListBySession(sessionID)
	if err != nil {
		return TodoListHTTPResult{}, err
	}
	items := todoDTOs(rows)
	return TodoListHTTPResult{Items: items, OpenCount: countOpenTodos(items)}, nil
}

func (s TodoService) ExecuteRuntimeTool(params methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	if err := validateRuntimeToolMeta(s.repos, params.RunID, params.SessionID, params.ToolCallID, true); err != nil {
		return methods.TodoToolExecuteResult{}, err
	}
	name := strings.TrimSpace(params.ToolName)
	switch name {
	case "todo.write", "todo_write":
		return s.executeWrite(params)
	case "todo.list":
		return s.executeList(params)
	default:
		return methods.TodoToolExecuteResult{}, fmt.Errorf("unsupported todo tool %q", name)
	}
}

func (s TodoService) FormatTodoContext(sessionID string) (*methods.TodoContext, error) {
	rows, err := s.repos.Todos.ListBySession(strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	// Open-first ordering for context budget.
	sorted := append([]model.TodoItem(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return todoStatusRank(sorted[i].Status) < todoStatusRank(sorted[j].Status)
	})
	items := make([]methods.TodoItemDTO, 0, len(sorted))
	lines := make([]string, 0, len(sorted))
	header := "Current session task list (update via todo.write when progress changes; keep one in_progress):\n"
	total := len(header)
	for _, row := range sorted {
		dto := todoItemDTO(row)
		line := formatTodoContextLine(row)
		if total+len(line)+1 > todoContextLimit {
			break
		}
		items = append(items, dto)
		lines = append(lines, line)
		total += len(line) + 1
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return &methods.TodoContext{
		Items:   items,
		Context: header + strings.Join(lines, "\n"),
	}, nil
}

func (s TodoService) executeList(params methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	rows, err := s.repos.Todos.ListBySession(params.SessionID)
	if err != nil {
		return methods.TodoToolExecuteResult{}, err
	}
	statusFilter := strings.ToLower(strings.TrimSpace(stringArgFromMap(params.Arguments, "status")))
	if statusFilter == "" {
		statusFilter = "all"
	}
	limit := intArgFromMap(params.Arguments, "limit", todoMaxItems)
	if limit <= 0 || limit > todoMaxItems {
		limit = todoMaxItems
	}
	filtered := make([]model.TodoItem, 0, len(rows))
	for _, row := range rows {
		if !todoStatusMatches(row.Status, statusFilter) {
			continue
		}
		filtered = append(filtered, row)
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	items := todoItemDTOs(filtered)
	open := countOpenTodoDTOs(items)
	output := runtimeTodoOutput("todo.list", items, open, map[string]any{
		"action": "list",
		"status": statusFilter,
	})
	return methods.TodoToolExecuteResult{
		Status:    "completed",
		Output:    output,
		Items:     items,
		OpenCount: open,
	}, nil
}

func (s TodoService) executeWrite(params methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	rawTodos, ok := params.Arguments["todos"]
	if !ok {
		return methods.TodoToolExecuteResult{}, fmt.Errorf("todos is required")
	}
	payloadItems, err := parseTodoPayloadItems(rawTodos)
	if err != nil {
		return methods.TodoToolExecuteResult{}, err
	}
	merge := true
	if v, ok := params.Arguments["merge"]; ok {
		switch t := v.(type) {
		case bool:
			merge = t
		case string:
			merge = strings.EqualFold(strings.TrimSpace(t), "true") || strings.TrimSpace(t) == ""
		}
	}

	now := time.Now().UTC()
	notes := make([]string, 0, 4)
	var final []model.TodoItem

	if !merge {
		final, notes, err = buildTodosReplace(params, payloadItems, now, notes)
	} else {
		existing, listErr := s.repos.Todos.ListBySession(params.SessionID)
		if listErr != nil {
			return methods.TodoToolExecuteResult{}, listErr
		}
		final, notes, err = buildTodosMerge(params, existing, payloadItems, now, notes)
	}
	if err != nil {
		return methods.TodoToolExecuteResult{}, err
	}
	if len(final) > todoMaxItems {
		return methods.TodoToolExecuteResult{}, fmt.Errorf("todo list exceeds max of %d items", todoMaxItems)
	}
	final, demoted := normalizeSingleInProgress(final, now)
	if demoted > 0 {
		notes = append(notes, fmt.Sprintf("demoted %d extra in_progress item(s) to pending", demoted))
	}

	if err := s.repos.Todos.ReplaceSession(params.SessionID, final); err != nil {
		return methods.TodoToolExecuteResult{}, err
	}

	items := todoItemDTOs(final)
	open := countOpenTodoDTOs(items)
	completed := 0
	cancelled := 0
	for _, item := range items {
		switch item.Status {
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}
	}
	text := fmt.Sprintf("Todos updated: %d open, %d completed, %d cancelled", open, completed, cancelled)
	if len(notes) > 0 {
		text += " (" + strings.Join(notes, "; ") + ")"
	}
	output := runtimeTodoOutput("todo.write", items, open, map[string]any{
		"action":          "write",
		"merge":           merge,
		"completed_count": completed,
		"cancelled_count": cancelled,
		"notes":           notes,
	})
	// Prefer human text for model; keep structured data in envelope via runtime standardization.
	// Gateway output is already standard envelope JSON with Text set.
	_ = text
	return methods.TodoToolExecuteResult{
		Status:    "completed",
		Output:    output,
		Items:     items,
		OpenCount: open,
	}, nil
}

type todoPayloadItem struct {
	ID         string
	ClientKey  string
	Content    string
	Status     string
	Priority   string
	ActiveForm string
}

func parseTodoPayloadItems(raw any) ([]todoPayloadItem, error) {
	arr, ok := raw.([]any)
	if !ok {
		// allow JSON array decoded as []map via re-marshal
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("todos must be an array")
		}
		var maps []map[string]any
		if err := json.Unmarshal(b, &maps); err != nil {
			return nil, fmt.Errorf("todos must be an array")
		}
		arr = make([]any, len(maps))
		for i, m := range maps {
			arr[i] = m
		}
	}
	out := make([]todoPayloadItem, 0, len(arr))
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("todos[%d] must be an object", i)
		}
		content := strings.TrimSpace(stringArgFromMap(m, "content"))
		if content == "" {
			return nil, fmt.Errorf("todos[%d].content is required", i)
		}
		if utf8.RuneCountInString(content) > todoMaxContentRunes {
			return nil, fmt.Errorf("todos[%d].content exceeds %d characters", i, todoMaxContentRunes)
		}
		status := strings.ToLower(strings.TrimSpace(stringArgFromMap(m, "status")))
		if status == "" {
			return nil, fmt.Errorf("todos[%d].status is required", i)
		}
		if !validTodoStatus(status) {
			return nil, fmt.Errorf("todos[%d].status must be pending|in_progress|completed|cancelled", i)
		}
		priority := strings.ToLower(strings.TrimSpace(stringArgFromMap(m, "priority")))
		if priority == "" {
			priority = "medium"
		} else if priority != "low" && priority != "medium" && priority != "high" {
			priority = "medium"
		}
		out = append(out, todoPayloadItem{
			ID:         strings.TrimSpace(stringArgFromMap(m, "id")),
			ClientKey:  strings.TrimSpace(stringArgFromMap(m, "client_key")),
			Content:    content,
			Status:     status,
			Priority:   priority,
			ActiveForm: strings.TrimSpace(stringArgFromMap(m, "active_form")),
		})
	}
	return out, nil
}

func buildTodosReplace(params methods.TodoToolExecuteParams, payload []todoPayloadItem, now time.Time, notes []string) ([]model.TodoItem, []string, error) {
	seenClientKeys := map[string]struct{}{}
	final := make([]model.TodoItem, 0, len(payload))
	for i, item := range payload {
		clientKey := assignClientKey(item, seenClientKeys)
		if clientKey != "" {
			seenClientKeys[clientKey] = struct{}{}
		}
		row := model.TodoItem{
			ID:               repository.NewTodoID(),
			ClientKey:        clientKey,
			SessionID:        params.SessionID,
			Content:          item.Content,
			Status:           item.Status,
			SortOrder:        i,
			Priority:         item.Priority,
			ActiveForm:       item.ActiveForm,
			SourceRunID:      params.RunID,
			SourceToolCallID: params.ToolCallID,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if item.Status == "completed" || item.Status == "cancelled" {
			row.CompletedAt = &now
		}
		final = append(final, row)
	}
	return final, notes, nil
}

func buildTodosMerge(params methods.TodoToolExecuteParams, existing []model.TodoItem, payload []todoPayloadItem, now time.Time, notes []string) ([]model.TodoItem, []string, error) {
	// Work on a value map keyed by Gateway PK so pointer lifetime is not an issue.
	byPK := map[string]model.TodoItem{}
	byClientKey := map[string]string{} // client_key -> pk
	for _, row := range existing {
		byPK[row.ID] = row
		if row.ClientKey != "" {
			byClientKey[row.ClientKey] = row.ID
		}
	}
	listed := map[string]struct{}{}
	listedOrder := make([]model.TodoItem, 0, len(payload))

	for i, item := range payload {
		matchPK := matchExistingTodoPK(item, byClientKey, byPK)
		var row model.TodoItem
		if matchPK != "" {
			row = byPK[matchPK]
			row.Content = item.Content
			row.Status = item.Status
			row.Priority = item.Priority
			row.ActiveForm = item.ActiveForm
			row.SortOrder = i
			row.SourceRunID = params.RunID
			row.SourceToolCallID = params.ToolCallID
			row.UpdatedAt = now
			if item.ClientKey != "" && item.ClientKey != row.ClientKey {
				if otherPK, clash := byClientKey[item.ClientKey]; !clash || otherPK == row.ID {
					if row.ClientKey != "" {
						delete(byClientKey, row.ClientKey)
					}
					row.ClientKey = item.ClientKey
				}
			}
			if item.Status == "completed" || item.Status == "cancelled" {
				if row.CompletedAt == nil {
					row.CompletedAt = &now
				}
			} else {
				row.CompletedAt = nil
			}
		} else {
			clientKey := ""
			if item.ClientKey != "" {
				if _, clash := byClientKey[item.ClientKey]; !clash {
					clientKey = item.ClientKey
				}
			} else if item.ID != "" {
				if _, clash := byClientKey[item.ID]; !clash {
					if _, asPK := byPK[item.ID]; !asPK {
						clientKey = item.ID
					}
				}
			}
			row = model.TodoItem{
				ID:               repository.NewTodoID(),
				ClientKey:        clientKey,
				SessionID:        params.SessionID,
				Content:          item.Content,
				Status:           item.Status,
				SortOrder:        i,
				Priority:         item.Priority,
				ActiveForm:       item.ActiveForm,
				SourceRunID:      params.RunID,
				SourceToolCallID: params.ToolCallID,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if item.Status == "completed" || item.Status == "cancelled" {
				row.CompletedAt = &now
			}
		}
		byPK[row.ID] = row
		if row.ClientKey != "" {
			byClientKey[row.ClientKey] = row.ID
		}
		listed[row.ID] = struct{}{}
		listedOrder = append(listedOrder, row)
	}

	kept := make([]model.TodoItem, 0)
	for _, row := range existing {
		if _, ok := listed[row.ID]; ok {
			continue
		}
		// use latest map value if mutated (should not be for unlisted)
		if current, ok := byPK[row.ID]; ok {
			kept = append(kept, current)
		} else {
			kept = append(kept, row)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].SortOrder == kept[j].SortOrder {
			return kept[i].CreatedAt.Before(kept[j].CreatedAt)
		}
		return kept[i].SortOrder < kept[j].SortOrder
	})
	base := len(listedOrder)
	for i := range kept {
		kept[i].SortOrder = base + i
		kept[i].UpdatedAt = now
	}
	final := append(listedOrder, kept...)
	return final, notes, nil
}

func matchExistingTodoPK(item todoPayloadItem, byClientKey map[string]string, byPK map[string]model.TodoItem) string {
	if item.ClientKey != "" {
		if pk, ok := byClientKey[item.ClientKey]; ok {
			return pk
		}
	}
	id := strings.TrimSpace(item.ID)
	if id == "" {
		return ""
	}
	if pk, ok := byClientKey[id]; ok {
		return pk
	}
	if _, ok := byPK[id]; ok {
		return id
	}
	return ""
}

func assignClientKey(item todoPayloadItem, seen map[string]struct{}) string {
	if item.ClientKey != "" {
		if _, ok := seen[item.ClientKey]; ok {
			return ""
		}
		return item.ClientKey
	}
	if item.ID != "" {
		// Do not use a gateway-looking id as client_key when it was meant as PK echo without match
		if _, ok := seen[item.ID]; ok {
			return ""
		}
		return item.ID
	}
	return ""
}

func normalizeSingleInProgress(items []model.TodoItem, now time.Time) ([]model.TodoItem, int) {
	lastIdx := -1
	count := 0
	for i, item := range items {
		if item.Status == "in_progress" {
			count++
			lastIdx = i
		}
	}
	if count <= 1 {
		return items, 0
	}
	demoted := 0
	for i := range items {
		if items[i].Status == "in_progress" && i != lastIdx {
			items[i].Status = "pending"
			items[i].CompletedAt = nil
			items[i].UpdatedAt = now
			demoted++
		}
	}
	return items, demoted
}

func runtimeTodoOutput(tool string, items []methods.TodoItemDTO, open int, data map[string]any) string {
	if data == nil {
		data = map[string]any{}
	}
	data["items"] = items
	data["open_count"] = open
	completed := 0
	cancelled := 0
	pending := 0
	inProgress := 0
	for _, item := range items {
		switch item.Status {
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		}
	}
	text := fmt.Sprintf("%s: %d items (in_progress=%d pending=%d completed=%d cancelled=%d)",
		tool, len(items), inProgress, pending, completed, cancelled)
	// Include client_key= id= pairs for model multi-turn matching.
	if len(items) > 0 && len(items) <= 12 {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			key := item.ClientKey
			if key == "" {
				key = "-"
			}
			parts = append(parts, fmt.Sprintf("client_key=%s id=%s status=%s", key, item.ID, item.Status))
		}
		text += "\n" + strings.Join(parts, "\n")
	}
	env := map[string]any{
		"schema": "red_panda.tool_result.v1",
		"tool":   tool,
		"status": "completed",
		"ok":     true,
		"text":   text,
		"data":   data,
		"meta":   map[string]any{},
	}
	raw, _ := json.Marshal(env)
	return string(raw)
}

func formatTodoContextLine(row model.TodoItem) string {
	key := row.ClientKey
	if key == "" {
		key = row.ID
	}
	line := fmt.Sprintf("- [%s] %s (%s)", row.Status, row.Content, key)
	runes := []rune(line)
	if len(runes) > todoMaxLineRunes {
		line = string(runes[:todoMaxLineRunes-1]) + "…"
	}
	return line
}

func todoStatusRank(status string) int {
	switch status {
	case "in_progress":
		return 0
	case "pending":
		return 1
	case "completed":
		return 2
	case "cancelled":
		return 3
	default:
		return 9
	}
}

func validTodoStatus(status string) bool {
	switch status {
	case "pending", "in_progress", "completed", "cancelled":
		return true
	default:
		return false
	}
}

func todoStatusMatches(status, filter string) bool {
	switch filter {
	case "all", "":
		return true
	case "open":
		return status == "pending" || status == "in_progress"
	default:
		return status == filter
	}
}

func todoDTOs(rows []model.TodoItem) []TodoDTO {
	out := make([]TodoDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, todoDTO(row))
	}
	return out
}

func todoDTO(row model.TodoItem) TodoDTO {
	return TodoDTO{
		ID:               row.ID,
		ClientKey:        row.ClientKey,
		SessionID:        row.SessionID,
		Content:          row.Content,
		Status:           row.Status,
		SortOrder:        row.SortOrder,
		Priority:         row.Priority,
		ActiveForm:       row.ActiveForm,
		SourceRunID:      row.SourceRunID,
		SourceToolCallID: row.SourceToolCallID,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		CompletedAt:      row.CompletedAt,
	}
}

func todoItemDTOs(rows []model.TodoItem) []methods.TodoItemDTO {
	out := make([]methods.TodoItemDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, todoItemDTO(row))
	}
	return out
}

func todoItemDTO(row model.TodoItem) methods.TodoItemDTO {
	return methods.TodoItemDTO{
		ID:         row.ID,
		ClientKey:  row.ClientKey,
		Content:    row.Content,
		Status:     row.Status,
		SortOrder:  row.SortOrder,
		Priority:   row.Priority,
		ActiveForm: row.ActiveForm,
	}
}

func countOpenTodos(items []TodoDTO) int {
	n := 0
	for _, item := range items {
		if item.Status == "pending" || item.Status == "in_progress" {
			n++
		}
	}
	return n
}

func countOpenTodoDTOs(items []methods.TodoItemDTO) int {
	n := 0
	for _, item := range items {
		if item.Status == "pending" || item.Status == "in_progress" {
			n++
		}
	}
	return n
}
