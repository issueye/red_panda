package workspace

import (
	"context"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	"redpanda/agent/plugins/core/workspace/internal"
	ptools "redpanda/protocol/tools"
)

// Register 将 workspace 域的 11 个工具注册到 Registry，返回注册的工具名列表。
func Register(reg *registry.Registry, bus *hooks.ExtensionBus) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		reg.Register(t)
		names = append(names, t.Definition.Name)
	}
	return names
}

// tools 是 workspace 域全部 11 个 ToolEntry，顺序与 dispatch_local.go 一致。
var tools = []registry.ToolEntry{
	{
		Definition: ptools.Definition{
			Name:        "workspace.read_file",
			DisplayName: "Read file",
			Description: "Read a text file inside the active workspace.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Workspace-relative file path."},
				},
				"required": []string{"path"},
			},
		},
		Handler:      runReadFileHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.list",
			DisplayName: "List files",
			Description: "List files and directories inside the active workspace.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Workspace-relative directory or file path."},
					"max_depth": map[string]any{"type": "integer", "description": "Maximum directory depth to include."},
				},
			},
		},
		Handler:      runListHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.stats",
			DisplayName: "Workspace stats",
			Description: "Summarize directory structure and file counts for split planning. Root agent MUST call this before multi-area analysis. Use total_files / top_level[].files to set each worker.delegate file_count or max_turns using formula max_turns = file_count + summary_turns (summary_turns is included as suggested_max_turns).",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Workspace-relative directory path. Defaults to workspace root."},
					"max_depth": map[string]any{"type": "integer", "description": "How deep to walk when counting (default 4, max 8)."},
				},
			},
		},
		Handler:      runStatsHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.grep",
			DisplayName: "Search files",
			Description: "Search text files inside the active workspace with a regular expression.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string", "description": "Regular expression to search for."},
					"path":        map[string]any{"type": "string", "description": "Workspace-relative directory or file path."},
					"max_matches": map[string]any{"type": "integer", "description": "Maximum number of matches to return."},
				},
				"required": []string{"pattern"},
			},
		},
		Handler:      runGrepHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.find_files",
			DisplayName: "Find files",
			Description: "Find files by filename, substring, or glob pattern inside the active workspace. Skips generated and dependency directories.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string", "description": "Filename substring or glob such as *.go or modules/**/*.jsx."},
					"path":        map[string]any{"type": "string", "description": "Workspace-relative directory to search. Defaults to workspace root."},
					"max_results": map[string]any{"type": "integer", "description": "Maximum matching paths to return (default 200, max 1000)."},
				},
				"required": []string{"pattern"},
			},
		},
		Handler:      runFindFilesHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.read_files",
			DisplayName: "Read files",
			Description: "Read up to 12 text files from the active workspace in one call, with clear per-file headings.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"paths": map[string]any{
						"type": "array", "description": "Workspace-relative text file paths.",
						"items": map[string]any{"type": "string"}, "maxItems": internal.DefaultReadFiles,
					},
				},
				"required": []string{"paths"},
			},
		},
		Handler:      runReadFilesHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.write_file",
			DisplayName: "Write file",
			Description: "Write text content to a file inside the active workspace.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Workspace-relative file path."},
					"content": map[string]any{"type": "string", "description": "Text content to write."},
				},
				"required": []string{"path", "content"},
			},
		},
		Handler:      runWriteFileHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.edit_file",
			DisplayName: "Edit file",
			Description: "Replace exact text inside a file in the active workspace.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "Workspace-relative file path."},
					"old_text":    map[string]any{"type": "string", "description": "Exact text to replace."},
					"new_text":    map[string]any{"type": "string", "description": "Replacement text."},
					"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences instead of requiring exactly one match."},
				},
				"required": []string{"path", "old_text", "new_text"},
			},
		},
		Handler:      runEditFileHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.diff_file",
			DisplayName: "Preview diff",
			Description: "Preview a unified diff for a proposed text change to one workspace file.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "Workspace-relative file path."},
					"content":     map[string]any{"type": "string", "description": "Full proposed file content."},
					"old_text":    map[string]any{"type": "string", "description": "Exact text to replace for preview."},
					"new_text":    map[string]any{"type": "string", "description": "Replacement text for preview."},
					"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences when previewing old_text/new_text."},
				},
				"required": []string{"path"},
			},
		},
		Handler:      runDiffFileHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
	{
		Definition: ptools.Definition{
			Name:        "workspace.apply_patch",
			DisplayName: "Apply patch",
			Description: "Apply a workspace-scoped unified patch to text files.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"patch": map[string]any{"type": "string", "description": "Unified diff patch to apply inside the workspace."},
				},
				"required": []string{"patch"},
			},
		},
		Handler:      runApplyPatchHandler,
		TimeoutClass: registry.LocalToolTimeout,
		Source:       "builtin:workspace",
	},
}

// ---------- handler wrappers ----------

func okResult(output string) *ptools.Result {
	return &ptools.Result{Status: ptools.CallStatusCompleted, Output: output}
}

func failResult(err error) *ptools.Result {
	return &ptools.Result{Status: ptools.CallStatusFailed, Error: err.Error()}
}

func runReadFileHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	relPath := internal.StringArg(args, "path")
	output, err := internal.RunReadFile(root, relPath)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runListHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	relPath := internal.StringArgDefault(args, "path", ".")
	maxDepth := internal.IntArg(args, "max_depth", internal.DefaultListDepth)
	output, err := internal.RunListWorkspace(root, relPath, maxDepth)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runStatsHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	relPath := internal.StringArgDefault(args, "path", ".")
	maxDepth := internal.IntArg(args, "max_depth", 4)
	output, err := internal.RunWorkspaceStats(root, relPath, maxDepth)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runGrepHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	pattern := internal.StringArg(args, "pattern")
	relPath := internal.StringArgDefault(args, "path", ".")
	maxMatches := internal.IntArg(args, "max_matches", internal.DefaultGrepMatches)
	output, err := internal.RunGrepWorkspace(root, pattern, relPath, maxMatches)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runFindFilesHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	relPath := internal.StringArgDefault(args, "path", ".")
	pattern := internal.StringArg(args, "pattern")
	maxResults := internal.IntArg(args, "max_results", internal.DefaultFindFiles)
	output, err := internal.RunFindFiles(root, relPath, pattern, maxResults)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runReadFilesHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	paths := internal.StringListArg(args, "paths")
	_ = paths
	output, err := internal.RunReadFiles(root, paths)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runWriteFileHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	relPath := internal.StringArg(args, "path")
	content := internal.StringArg(args, "content")
	output, err := internal.RunWriteFile(root, relPath, content)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runEditFileHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	relPath := internal.StringArg(args, "path")
	oldText := internal.StringArg(args, "old_text")
	newText := internal.StringArg(args, "new_text")
	replaceAll := internal.BoolArg(args, "replace_all", false)
	output, err := internal.RunEditFile(root, relPath, oldText, newText, replaceAll)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runDiffFileHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	_ = args
	root := toolCtx.WorkingDir
	relPath := internal.StringArg(args, "path")
	content, hasContent := internal.StringArgPresent(args, "content")
	oldText := internal.StringArg(args, "old_text")
	newText := internal.StringArg(args, "new_text")
	replaceAll := internal.BoolArg(args, "replace_all", false)
	output, err := internal.RunDiffFile(root, relPath, content, hasContent, oldText, newText, replaceAll)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}

func runApplyPatchHandler(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	root := toolCtx.WorkingDir
	patch := internal.StringArg(args, "patch")
	output, err := internal.RunApplyPatch(root, patch)
	if err != nil {
		return failResult(err), err
	}
	return okResult(output), nil
}
