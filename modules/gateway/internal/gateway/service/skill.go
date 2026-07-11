package service

import (
	"context"
	"fmt"
	"strings"

	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/protocol/methods"
)

type SkillService struct {
	runtime *runtimeclient.Client
}

func NewSkillService(runtime *runtimeclient.Client) SkillService {
	return SkillService{runtime: runtime}
}

func (s SkillService) List(ctx context.Context, workspaceRoot string) (methods.SkillsListResult, error) {
	if err := s.requireRuntime(); err != nil {
		return methods.SkillsListResult{}, err
	}
	root, err := requireWorkspaceRoot(workspaceRoot)
	if err != nil {
		return methods.SkillsListResult{}, err
	}
	return s.runtime.ListSkills(ctx, methods.SkillsListParams{WorkspaceRoot: root})
}

func (s SkillService) Get(ctx context.Context, workspaceRoot string, name string, includeInstructions bool) (methods.SkillLoadResult, error) {
	if err := s.requireRuntime(); err != nil {
		return methods.SkillLoadResult{}, err
	}
	root, err := requireWorkspaceRoot(workspaceRoot)
	if err != nil {
		return methods.SkillLoadResult{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return methods.SkillLoadResult{}, fmt.Errorf("skill name is required")
	}
	return s.runtime.LoadSkill(ctx, methods.SkillLoadParams{
		WorkspaceRoot:       root,
		Name:                name,
		IncludeInstructions: includeInstructions,
	})
}

func (s SkillService) Create(ctx context.Context, params methods.SkillMutateParams) (methods.SkillMutateResult, error) {
	if err := s.requireRuntime(); err != nil {
		return methods.SkillMutateResult{}, err
	}
	root, err := requireWorkspaceRoot(params.WorkspaceRoot)
	if err != nil {
		return methods.SkillMutateResult{}, err
	}
	params.WorkspaceRoot = root
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		return methods.SkillMutateResult{}, fmt.Errorf("skill name is required")
	}
	return s.runtime.CreateSkill(ctx, params)
}

func (s SkillService) Update(ctx context.Context, name string, params methods.SkillMutateParams) (methods.SkillMutateResult, error) {
	if err := s.requireRuntime(); err != nil {
		return methods.SkillMutateResult{}, err
	}
	root, err := requireWorkspaceRoot(params.WorkspaceRoot)
	if err != nil {
		return methods.SkillMutateResult{}, err
	}
	params.WorkspaceRoot = root
	params.Name = strings.TrimSpace(name)
	if params.Name == "" {
		return methods.SkillMutateResult{}, fmt.Errorf("skill name is required")
	}
	return s.runtime.UpdateSkill(ctx, params)
}

func (s SkillService) Delete(ctx context.Context, workspaceRoot string, name string) (methods.SkillDeleteResult, error) {
	if err := s.requireRuntime(); err != nil {
		return methods.SkillDeleteResult{}, err
	}
	root, err := requireWorkspaceRoot(workspaceRoot)
	if err != nil {
		return methods.SkillDeleteResult{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return methods.SkillDeleteResult{}, fmt.Errorf("skill name is required")
	}
	return s.runtime.DeleteSkill(ctx, methods.SkillDeleteParams{
		WorkspaceRoot: root,
		Name:          name,
	})
}

func (s SkillService) requireRuntime() error {
	if s.runtime == nil {
		return fmt.Errorf("runtime client not configured")
	}
	return nil
}

func requireWorkspaceRoot(workspaceRoot string) (string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return "", fmt.Errorf("workspace_root is required")
	}
	return root, nil
}
