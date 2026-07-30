package coding

import (
	"context"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	"redpanda/agent/plugins/core/coding/internal"
	ptools "redpanda/protocol/tools"
)

func Register(reg *registry.Registry, bus *hooks.ExtensionBus) []string {
	names := []string{
		"git.status", "git.diff", "git.log", "git.show", "shell.exec",
	}

	entries := []registry.ToolEntry{
		{
			Definition: ptools.Definition{
				Name:        "git.status",
				DisplayName: "Git status",
				Description: "Show the current branch and concise working tree status without modifying the repository.",
				Risk:        ptools.RiskLow,
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
			Handler: func(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				output, err := internal.RunGitStatus(ctx, tc.WorkingDir)
				return internal.WrapResult("git.status", output, err)
			},
			TimeoutClass: registry.LocalToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:coding",
		},
		{
			Definition: ptools.Definition{
				Name:        "git.diff",
				DisplayName: "Git diff",
				Description: "Show an unstaged, staged, or revision-based Git diff, optionally limited to one workspace path.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"staged":   map[string]any{"type": "boolean", "description": "Show staged changes."},
						"revision": map[string]any{"type": "string", "description": "Optional revision or range such as HEAD~1 or main...HEAD."},
						"path":     map[string]any{"type": "string", "description": "Optional workspace-relative path."},
					},
				},
			},
			Handler: func(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				output, err := internal.RunGitDiff(ctx, tc.WorkingDir,
					internal.BoolArg(args, "staged", false),
					internal.StrArg(args, "revision"),
					internal.StrArg(args, "path"))
				return internal.WrapResult("git.diff", output, err)
			},
			TimeoutClass: registry.LocalToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:coding",
		},
		{
			Definition: ptools.Definition{
				Name:        "git.log",
				DisplayName: "Git log",
				Description: "Show recent commits in a concise format, optionally limited to one workspace path.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"max_count": map[string]any{"type": "integer", "description": "Maximum commits to return (default 10, max 50)."},
						"path":      map[string]any{"type": "string", "description": "Optional workspace-relative path."},
					},
				},
			},
			Handler: func(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				output, err := internal.RunGitLog(ctx, tc.WorkingDir,
					internal.IntArg(args, "max_count", 10),
					internal.StrArg(args, "path"))
				return internal.WrapResult("git.log", output, err)
			},
			TimeoutClass: registry.LocalToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:coding",
		},
		{
			Definition: ptools.Definition{
				Name:        "git.show",
				DisplayName: "Git show",
				Description: "Show commit metadata and patch for a revision, optionally limited to one workspace path.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"revision": map[string]any{"type": "string", "description": "Revision to show. Defaults to HEAD."},
						"path":     map[string]any{"type": "string", "description": "Optional workspace-relative path."},
					},
				},
			},
			Handler: func(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				output, err := internal.RunGitShow(ctx, tc.WorkingDir,
					internal.StrArg(args, "revision"),
					internal.StrArg(args, "path"))
				return internal.WrapResult("git.show", output, err)
			},
			TimeoutClass: registry.LocalToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:coding",
		},
		{
			Definition: ptools.Definition{
				Name:        "shell.exec",
				DisplayName: "Shell",
				Description: "Run a shell command in the active workspace.",
				Risk:        ptools.RiskHigh,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string", "description": "Command to execute."},
					},
					"required": []string{"command"},
				},
			},
			Handler: func(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				output, err := internal.RunShell(ctx, tc.WorkingDir,
					internal.StrArg(args, "command"))
				return internal.WrapResult("shell.exec", output, err)
			},
			TimeoutClass: registry.SelfManagedToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:coding",
		},
	}

	for _, e := range entries {
		reg.MustRegister(e)
	}

	return names
}
