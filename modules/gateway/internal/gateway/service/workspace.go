package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

type WorkspaceService struct {
	repos repository.Set
}

type WorkspaceDTO struct {
	ID           string    `json:"id"`
	Root         string    `json:"root"`
	RootPath     string    `json:"root_path"`
	Name         string    `json:"name"`
	LastOpenedAt time.Time `json:"last_opened_at"`
}

func NewWorkspaceService(repos repository.Set) WorkspaceService {
	return WorkspaceService{repos: repos}
}

func (s WorkspaceService) Open(root string) (WorkspaceDTO, error) {
	workspace, err := s.repos.Workspaces.Upsert(filepath.Clean(root))
	if err != nil {
		return WorkspaceDTO{}, err
	}
	return workspaceDTO(workspace), nil
}

func (s WorkspaceService) Current() (*WorkspaceDTO, error) {
	workspace, ok, err := s.repos.Workspaces.Current()
	if err != nil || !ok {
		return nil, err
	}
	dto := workspaceDTO(workspace)
	return &dto, nil
}

func (s WorkspaceService) Recent() ([]WorkspaceDTO, error) {
	rows, err := s.repos.Workspaces.Recent(20)
	if err != nil {
		return nil, err
	}
	items := make([]WorkspaceDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, workspaceDTO(row))
	}
	return items, nil
}

// Remove deletes a workspace from the recent list and soft-deletes its sessions.
func (s WorkspaceService) Remove(id string, deleteSessions bool) (WorkspaceDTO, int64, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return WorkspaceDTO{}, 0, fmt.Errorf("workspace id is required")
	}
	workspace, err := s.repos.Workspaces.Get(id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return WorkspaceDTO{}, 0, fmt.Errorf("workspace not found")
		}
		return WorkspaceDTO{}, 0, err
	}
	var deletedSessions int64
	if deleteSessions {
		// Drop todos before soft-deleting sessions so workspace_root lookup still works.
		_ = s.repos.Todos.DeleteByWorkspaceRoot(workspace.Root)
		deletedSessions, err = s.repos.Sessions.SoftDeleteByWorkspace(workspace.Root)
		if err != nil {
			return WorkspaceDTO{}, 0, err
		}
	}
	if err := s.repos.Workspaces.Delete(id); err != nil {
		return WorkspaceDTO{}, 0, err
	}
	return workspaceDTO(workspace), deletedSessions, nil
}

func workspaceDTO(row model.Workspace) WorkspaceDTO {
	return WorkspaceDTO{
		ID:           row.ID,
		Root:         row.Root,
		RootPath:     row.Root,
		Name:         row.Name,
		LastOpenedAt: row.LastOpenedAt,
	}
}
