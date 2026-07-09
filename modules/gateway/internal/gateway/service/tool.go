package service

import (
	"encoding/json"
	"time"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

type ToolService struct {
	repos repository.Set
}

type ToolCallDTO struct {
	ID           string         `json:"id"`
	RootRunID    string         `json:"root_run_id"`
	SessionID    string         `json:"session_id"`
	AgentID      string         `json:"agent_id,omitempty"`
	AgentRole    string         `json:"agent_role,omitempty"`
	ToolName     string         `json:"tool_name"`
	DisplayName  string         `json:"display_name,omitempty"`
	Risk         string         `json:"risk,omitempty"`
	Policy       string         `json:"policy,omitempty"`
	PolicyReason string         `json:"policy_reason,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
	Status       string         `json:"status"`
	Output       string         `json:"output,omitempty"`
	Error        string         `json:"error,omitempty"`
	ExitCode     int            `json:"exit_code,omitempty"`
	DurationMS   int64          `json:"duration_ms,omitempty"`
	StartedSeq   uint64         `json:"started_seq"`
	FinishedSeq  uint64         `json:"finished_seq,omitempty"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   *time.Time     `json:"finished_at,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func NewToolService(repos repository.Set) ToolService {
	return ToolService{repos: repos}
}

func (s ToolService) ListByRun(rootRunID string, limit int) ([]ToolCallDTO, error) {
	rows, err := s.repos.ToolCalls.ListByRun(rootRunID, limit)
	if err != nil {
		return nil, err
	}
	return toolCallDTOs(rows)
}

func (s ToolService) ListBySession(sessionID string, limit int) ([]ToolCallDTO, error) {
	rows, err := s.repos.ToolCalls.ListBySession(sessionID, limit)
	if err != nil {
		return nil, err
	}
	return toolCallDTOs(rows)
}

func toolCallDTOs(rows []model.ToolCall) ([]ToolCallDTO, error) {
	items := make([]ToolCallDTO, 0, len(rows))
	for _, row := range rows {
		item, err := toolCallDTO(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func toolCallDTO(row model.ToolCall) (ToolCallDTO, error) {
	var args map[string]any
	if row.ArgumentsJSON != "" && row.ArgumentsJSON != "null" {
		if err := json.Unmarshal([]byte(row.ArgumentsJSON), &args); err != nil {
			return ToolCallDTO{}, err
		}
	}
	return ToolCallDTO{
		ID:           row.ID,
		RootRunID:    row.RootRunID,
		SessionID:    row.SessionID,
		AgentID:      row.AgentID,
		AgentRole:    row.AgentRole,
		ToolName:     row.ToolName,
		DisplayName:  row.DisplayName,
		Risk:         row.Risk,
		Policy:       row.Policy,
		PolicyReason: row.PolicyReason,
		Arguments:    args,
		Status:       row.Status,
		Output:       row.Output,
		Error:        row.Error,
		ExitCode:     row.ExitCode,
		DurationMS:   row.DurationMS,
		StartedSeq:   row.StartedSeq,
		FinishedSeq:  row.FinishedSeq,
		StartedAt:    row.StartedAt,
		FinishedAt:   row.FinishedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}
