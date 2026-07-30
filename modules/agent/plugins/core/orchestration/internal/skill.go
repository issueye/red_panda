package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"redpanda/agent/internal/skill"
)

func skillHandlerList(_ context.Context, toolCtx *ToolContext, args map[string]any) (*Result, error) {
	wd := toolCtx.WorkingDir
	if wd == "" {
		return nil, errors.New("working directory is required")
	}
	output, err := skill.RunList(wd)
	if err != nil {
		return nil, err
	}
	return &Result{Output: output}, nil
}

func skillHandlerCreate(_ context.Context, toolCtx *ToolContext, args map[string]any) (*Result, error) {
	wd := toolCtx.WorkingDir
	if wd == "" {
		return nil, errors.New("working directory is required")
	}
	name := strings.TrimSpace(StringArg(args, "name"))
	desc := strings.TrimSpace(StringArg(args, "description"))
	instr := StringArg(args, "instructions")
	output, err := skill.RunCreate(wd, name, desc, instr)
	if err != nil {
		return nil, err
	}
	return &Result{Output: output}, nil
}

func skillHandlerUpdate(_ context.Context, toolCtx *ToolContext, args map[string]any) (*Result, error) {
	wd := toolCtx.WorkingDir
	if wd == "" {
		return nil, errors.New("working directory is required")
	}
	name := strings.TrimSpace(StringArg(args, "name"))
	desc := strings.TrimSpace(StringArg(args, "description"))
	instr := StringArg(args, "instructions")
	output, err := skill.RunUpdate(wd, name, desc, instr)
	if err != nil {
		return nil, err
	}
	return &Result{Output: output}, nil
}

func skillHandlerDelete(_ context.Context, toolCtx *ToolContext, args map[string]any) (*Result, error) {
	wd := toolCtx.WorkingDir
	if wd == "" {
		return nil, errors.New("working directory is required")
	}
	name := strings.TrimSpace(StringArg(args, "name"))
	output, err := skill.RunDelete(wd, name)
	if err != nil {
		return nil, err
	}
	return &Result{Output: output}, nil
}

func skillHandlerRun(_ context.Context, _ *ToolContext, _ map[string]any) (*Result, error) {
	// skill.run requires the Runtime (executeDelegatedAssignment,
	// delegatedWorkerReply, loadManagedSkillLocal) which lives in
	// agent/internal/runtime. Return a placeholder so the plugin compiles;
	// the real handler is wired through the Runtime.
	return nil, fmt.Errorf("not yet migrated")
}

// Handler exports.
var (
	HandlerSkillList HandlerFunc = skillHandlerList
	HandlerSkillCreate HandlerFunc = skillHandlerCreate
	HandlerSkillUpdate HandlerFunc = skillHandlerUpdate
	HandlerSkillDelete HandlerFunc = skillHandlerDelete
	HandlerSkillRun  HandlerFunc = skillHandlerRun
)
