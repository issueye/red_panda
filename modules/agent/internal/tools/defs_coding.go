package tools

// defs_coding.go — git.* and shell.* tool definitions
// (docs/plans/2026-07-19-convergence-wave.md Wave C Task C2).
// Order is preserved by the aggregator in registry.go.

import (
	ptools "redpanda/protocol/tools"
)

func codingToolDefinitions() []ptools.Definition {
	return []ptools.Definition{
		{
			Name:        "git.status",
			DisplayName: "Git status",
			Description: "Show the current branch and concise working tree status without modifying the repository.",
			Risk:        ptools.RiskLow,
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
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
		{
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
		{
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
		{
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
	}
}
