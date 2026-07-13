package service

import (
	"encoding/json"
	"time"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

type PermissionService struct {
	repos repository.Set
}

type PermissionRequestDTO struct {
	ID         string         `json:"id"`
	RunID      string         `json:"run_id"`
	SessionID  string         `json:"session_id,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	Risk       string         `json:"risk,omitempty"`
	Summary    string         `json:"summary"`
	Detail     string         `json:"detail,omitempty"`
	Arguments  map[string]any `json:"arguments,omitempty"`
	Status     string         `json:"status"`
	Decision   string         `json:"decision,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	RunSeq     uint64         `json:"run_seq,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

func NewPermissionService(repos repository.Set) PermissionService {
	return PermissionService{repos: repos}
}

func (s PermissionService) Get(permissionID string) (PermissionRequestDTO, error) {
	row, err := s.repos.Permissions.Get(permissionID)
	if err != nil {
		return PermissionRequestDTO{}, err
	}
	return permissionDTO(row)
}

func (s PermissionService) Pending(limit int) ([]PermissionRequestDTO, error) {
	rows, err := s.repos.Permissions.ListPending(limit)
	if err != nil {
		return nil, err
	}
	return permissionDTOs(rows)
}

func (s PermissionService) ListByRun(runID string, limit int) ([]PermissionRequestDTO, error) {
	rows, err := s.repos.Permissions.ListByRun(runID, limit)
	if err != nil {
		return nil, err
	}
	return permissionDTOs(rows)
}

func (s PermissionService) ListBySession(sessionID string, limit int) ([]PermissionRequestDTO, error) {
	rows, err := s.repos.Permissions.ListBySession(sessionID, limit)
	if err != nil {
		return nil, err
	}
	return permissionDTOs(rows)
}

func permissionDTOs(rows []model.PermissionRequest) ([]PermissionRequestDTO, error) {
	items := make([]PermissionRequestDTO, 0, len(rows))
	for _, row := range rows {
		item, err := permissionDTO(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func permissionDTO(row model.PermissionRequest) (PermissionRequestDTO, error) {
	var args map[string]any
	if row.ArgumentsJSON != "" && row.ArgumentsJSON != "null" {
		if err := json.Unmarshal([]byte(row.ArgumentsJSON), &args); err != nil {
			return PermissionRequestDTO{}, err
		}
	}
	return PermissionRequestDTO{
		ID:         row.ID,
		RunID:      row.RunID,
		SessionID:  row.SessionID,
		ToolCallID: row.ToolCallID,
		ToolName:   row.ToolName,
		Risk:       row.Risk,
		Summary:    row.Summary,
		Detail:     row.Detail,
		Arguments:  args,
		Status:     row.Status,
		Decision:   row.Decision,
		Reason:     row.Reason,
		RunSeq:     row.RunSeq,
		CreatedAt:  row.CreatedAt,
		ResolvedAt: row.ResolvedAt,
		UpdatedAt:  row.UpdatedAt,
	}, nil
}
