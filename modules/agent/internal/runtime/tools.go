package runtime

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"time"

	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

const maxToolOutputBytes = 64 * 1024
const maxGrepFileBytes = 2 * 1024 * 1024
const defaultListDepth = 3
const maxListEntries = 500
const defaultGrepMatches = 100
const maxPatchBytes = 256 * 1024

type MemoryToolExecutor func(context.Context, methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error)
type SkillRunExecutor func(context.Context, ToolRunContext, tools.Call) (string, error)

type ToolRunner struct {
	MemoryExecutor MemoryToolExecutor
	SkillExecutor  SkillRunExecutor
}

type ToolRunContext struct {
	WorkingDir string
	RunID      string
	SessionID  string
	Reply      *methods.ReplyParams
}

type ToolInvocation struct {
	Call tools.Call
}

func (ToolRunner) AvailableTools() []tools.Definition {
	return []tools.Definition{
		{
			Name:        "workspace.read_file",
			DisplayName: "Read file",
			Description: "Read a text file inside the active workspace.",
			Risk:        tools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Workspace-relative file path."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "workspace.list",
			DisplayName: "List files",
			Description: "List files and directories inside the active workspace.",
			Risk:        tools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Workspace-relative directory or file path."},
					"max_depth": map[string]any{"type": "integer", "description": "Maximum directory depth to include."},
				},
			},
		},
		{
			Name:        "workspace.grep",
			DisplayName: "Search files",
			Description: "Search text files inside the active workspace with a regular expression.",
			Risk:        tools.RiskLow,
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
		{
			Name:        "workspace.write_file",
			DisplayName: "Write file",
			Description: "Write text content to a file inside the active workspace.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Workspace-relative file path."},
					"content": map[string]any{"type": "string", "description": "Text content to write."},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "workspace.edit_file",
			DisplayName: "Edit file",
			Description: "Replace exact text inside a file in the active workspace.",
			Risk:        tools.RiskHigh,
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
		{
			Name:        "workspace.diff_file",
			DisplayName: "Preview diff",
			Description: "Preview a unified diff for a proposed text change to one workspace file.",
			Risk:        tools.RiskLow,
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
		{
			Name:        "workspace.apply_patch",
			DisplayName: "Apply patch",
			Description: "Apply a workspace-scoped unified patch to text files.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"patch": map[string]any{"type": "string", "description": "Unified diff patch to apply inside the workspace."},
				},
				"required": []string{"patch"},
			},
		},
		{
			Name:        "shell.exec",
			DisplayName: "Shell",
			Description: "Run a shell command in the active workspace.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Command to execute."},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "skill.create",
			DisplayName: "Create skill",
			Description: "Create a managed SKILL.md under .codex/skills in the active workspace.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":         map[string]any{"type": "string", "description": "Lowercase skill name using letters, digits, and hyphens."},
					"description":  map[string]any{"type": "string", "description": "Short description used to select the skill."},
					"instructions": map[string]any{"type": "string", "description": "Complete Markdown instructions for the skill."},
				},
				"required": []string{"name", "description", "instructions"},
			},
		},
		{
			Name:        "skill.update",
			DisplayName: "Update skill",
			Description: "Replace an existing managed SKILL.md under .codex/skills in the active workspace.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":         map[string]any{"type": "string", "description": "Existing lowercase skill name."},
					"description":  map[string]any{"type": "string", "description": "Replacement description used to select the skill."},
					"instructions": map[string]any{"type": "string", "description": "Complete replacement Markdown instructions."},
				},
				"required": []string{"name", "description", "instructions"},
			},
		},
		{
			Name:        "skill.run",
			DisplayName: "Run skill",
			Description: "Run a managed workspace skill in an isolated runtime-process subagent and return only its final result.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Existing managed skill name."},
					"task": map[string]any{"type": "string", "description": "Task for the isolated skill subagent."},
				},
				"required": []string{"name", "task"},
			},
		},
		{
			Name:        "memory.list",
			DisplayName: "List memory",
			Description: "List active or disabled project/session memory visible to the current run.",
			Risk:        tools.RiskMedium,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope":  map[string]any{"type": "string", "description": "Optional memory scope: project or session."},
					"status": map[string]any{"type": "string", "description": "Optional status: active or disabled."},
					"limit":  map[string]any{"type": "integer", "description": "Maximum number of records to return."},
				},
			},
		},
		{
			Name:        "memory.create",
			DisplayName: "Create memory",
			Description: "Create a project or session memory record for future runs.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope":      map[string]any{"type": "string", "description": "Memory scope: project or session."},
					"kind":       map[string]any{"type": "string", "description": "Memory kind."},
					"title":      map[string]any{"type": "string", "description": "Short title."},
					"content":    map[string]any{"type": "string", "description": "Memory content."},
					"confidence": map[string]any{"type": "string", "description": "Confidence: low, medium, or high."},
				},
				"required": []string{"scope", "content"},
			},
		},
		{
			Name:        "memory.update",
			DisplayName: "Update memory",
			Description: "Update a project or session memory record visible to the current run.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":         map[string]any{"type": "string", "description": "Memory record id."},
					"kind":       map[string]any{"type": "string", "description": "Memory kind."},
					"status":     map[string]any{"type": "string", "description": "Status: active or disabled."},
					"title":      map[string]any{"type": "string", "description": "Short title."},
					"content":    map[string]any{"type": "string", "description": "Memory content."},
					"confidence": map[string]any{"type": "string", "description": "Confidence: low, medium, or high."},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "memory.delete",
			DisplayName: "Delete memory",
			Description: "Soft-delete a project or session memory record visible to the current run.",
			Risk:        tools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Memory record id."},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (ToolRunner) Parse(text string, runID string) (ToolInvocation, bool) {
	line := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(line, "/tool read "):
		return readInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool read "))), true
	case strings.HasPrefix(line, "/read "):
		return readInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/read "))), true
	case line == "/tool list" || strings.HasPrefix(line, "/tool list "):
		return listInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool list"))), true
	case line == "/list" || strings.HasPrefix(line, "/list "):
		return listInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/list"))), true
	case strings.HasPrefix(line, "/tool grep "):
		return grepInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool grep "))), true
	case strings.HasPrefix(line, "/grep "):
		return grepInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/grep "))), true
	case strings.HasPrefix(line, "/tool shell "):
		return shellInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool shell "))), true
	case strings.HasPrefix(line, "/shell "):
		return shellInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/shell "))), true
	case strings.HasPrefix(line, "/tool write "):
		return writeInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool write "))), true
	case strings.HasPrefix(line, "/write "):
		return writeInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/write "))), true
	case strings.HasPrefix(line, "/tool edit "):
		return editInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool edit "))), true
	case strings.HasPrefix(line, "/edit "):
		return editInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/edit "))), true
	case strings.HasPrefix(line, "/tool diff "):
		return diffInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool diff "))), true
	case strings.HasPrefix(line, "/diff "):
		return diffInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/diff "))), true
	case strings.HasPrefix(line, "/tool patch "):
		return patchInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool patch "))), true
	case strings.HasPrefix(line, "/patch "):
		return patchInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/patch "))), true
	default:
		return ToolInvocation{}, false
	}
}

func (runner ToolRunner) InvocationFromCall(runID string, index int, call tools.Call) (ToolInvocation, error) {
	if call.Name == "" {
		return ToolInvocation{}, fmt.Errorf("tool name is required")
	}
	if call.ID == "" {
		call.ID = fmt.Sprintf("tool_%s_model_%d", runID, index+1)
	}
	for _, definition := range runner.AvailableTools() {
		if definition.Name == call.Name {
			if call.DisplayName == "" {
				call.DisplayName = definition.DisplayName
			}
			if call.Risk == "" {
				call.Risk = definition.Risk
			}
			if call.Arguments == nil {
				call.Arguments = map[string]any{}
			}
			return ToolInvocation{Call: call}, nil
		}
	}
	return ToolInvocation{}, fmt.Errorf("unknown tool %s", call.Name)
}

func (runner ToolRunner) Run(ctx context.Context, workingDir string, invocation ToolInvocation) (tools.Result, string) {
	return runner.RunWithContext(ctx, ToolRunContext{WorkingDir: workingDir}, invocation)
}

func (runner ToolRunner) RunWithContext(ctx context.Context, runCtx ToolRunContext, invocation ToolInvocation) (tools.Result, string) {
	started := time.Now()
	call := invocation.Call
	result := tools.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Status:     tools.CallStatusCompleted,
	}

	var output string
	var err error
	switch call.Name {
	case "workspace.read_file":
		output, err = runReadFile(runCtx.WorkingDir, stringArg(call.Arguments, "path"))
	case "workspace.list":
		output, err = runListWorkspace(runCtx.WorkingDir, stringArgDefault(call.Arguments, "path", "."), intArg(call.Arguments, "max_depth", defaultListDepth))
	case "workspace.grep":
		output, err = runGrepWorkspace(runCtx.WorkingDir, stringArg(call.Arguments, "pattern"), stringArgDefault(call.Arguments, "path", "."), intArg(call.Arguments, "max_matches", defaultGrepMatches))
	case "workspace.write_file":
		output, err = runWriteFile(runCtx.WorkingDir, stringArg(call.Arguments, "path"), stringArg(call.Arguments, "content"))
	case "workspace.edit_file":
		output, err = runEditFile(runCtx.WorkingDir, stringArg(call.Arguments, "path"), stringArg(call.Arguments, "old_text"), stringArg(call.Arguments, "new_text"), boolArg(call.Arguments, "replace_all", false))
	case "workspace.diff_file":
		content, hasContent := stringArgPresent(call.Arguments, "content")
		output, err = runDiffFile(runCtx.WorkingDir, stringArg(call.Arguments, "path"), content, hasContent, stringArg(call.Arguments, "old_text"), stringArg(call.Arguments, "new_text"), boolArg(call.Arguments, "replace_all", false))
	case "workspace.apply_patch":
		output, err = runApplyPatch(runCtx.WorkingDir, stringArg(call.Arguments, "patch"))
	case "shell.exec":
		output, err = runShell(ctx, runCtx.WorkingDir, stringArg(call.Arguments, "command"))
	case "skill.create":
		output, err = runCreateSkill(runCtx.WorkingDir, stringArg(call.Arguments, "name"), stringArg(call.Arguments, "description"), stringArg(call.Arguments, "instructions"))
	case "skill.update":
		output, err = runUpdateSkill(runCtx.WorkingDir, stringArg(call.Arguments, "name"), stringArg(call.Arguments, "description"), stringArg(call.Arguments, "instructions"))
	case "skill.run":
		if runner.SkillExecutor == nil {
			err = fmt.Errorf("skill subagent executor is not available")
		} else {
			output, err = runner.SkillExecutor(ctx, runCtx, call)
		}
	case "memory.list", "memory.create", "memory.update", "memory.delete":
		output, err = runner.runMemoryTool(ctx, runCtx, call)
	default:
		err = fmt.Errorf("unknown tool %s", call.Name)
	}

	result.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Status = tools.CallStatusFailed
		result.Error = err.Error()
		return result, err.Error()
	}
	result.Output = output
	return result, output
}

func (runner ToolRunner) runMemoryTool(ctx context.Context, runCtx ToolRunContext, call tools.Call) (string, error) {
	if runner.MemoryExecutor == nil {
		return "", fmt.Errorf("memory tool executor is not available")
	}
	result, err := runner.MemoryExecutor(ctx, methods.MemoryToolExecuteParams{
		RunID:         runCtx.RunID,
		SessionID:     runCtx.SessionID,
		WorkspaceRoot: runCtx.WorkingDir,
		ToolCallID:    call.ID,
		ToolName:      call.Name,
		Arguments:     call.Arguments,
	})
	if err != nil {
		return "", err
	}
	if result.Status != "" && result.Status != "completed" {
		return result.Output, fmt.Errorf("memory tool returned status %s", result.Status)
	}
	return result.Output, nil
}

func readInvocation(runID string, path string) ToolInvocation {
	return ToolInvocation{Call: tools.Call{
		ID:          "tool_" + runID + "_read",
		Name:        "workspace.read_file",
		DisplayName: "Read file",
		Risk:        tools.RiskLow,
		Arguments:   map[string]any{"path": path},
	}}
}

func listInvocation(runID string, path string) ToolInvocation {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	return ToolInvocation{Call: tools.Call{
		ID:          "tool_" + runID + "_list",
		Name:        "workspace.list",
		DisplayName: "List files",
		Risk:        tools.RiskLow,
		Arguments:   map[string]any{"path": path, "max_depth": defaultListDepth},
	}}
}

func grepInvocation(runID string, rest string) ToolInvocation {
	pattern, path, ok := strings.Cut(strings.TrimSpace(rest), " ")
	if !ok {
		path = "."
	}
	return ToolInvocation{Call: tools.Call{
		ID:          "tool_" + runID + "_grep",
		Name:        "workspace.grep",
		DisplayName: "Search files",
		Risk:        tools.RiskLow,
		Arguments:   map[string]any{"pattern": pattern, "path": strings.TrimSpace(path), "max_matches": defaultGrepMatches},
	}}
}

func shellInvocation(runID string, command string) ToolInvocation {
	return ToolInvocation{Call: tools.Call{
		ID:          "tool_" + runID + "_shell",
		Name:        "shell.exec",
		DisplayName: "Shell",
		Risk:        tools.RiskHigh,
		Arguments:   map[string]any{"command": command},
	}}
}

func writeInvocation(runID string, rest string) ToolInvocation {
	path, content, ok := strings.Cut(rest, " ")
	if !ok {
		path = rest
		content = ""
	}
	return ToolInvocation{Call: tools.Call{
		ID:          "tool_" + runID + "_write",
		Name:        "workspace.write_file",
		DisplayName: "Write file",
		Risk:        tools.RiskHigh,
		Arguments:   map[string]any{"path": path, "content": content},
	}}
}

func editInvocation(runID string, rest string) ToolInvocation {
	path, replacement, ok := strings.Cut(rest, " ")
	oldText, newText := "", ""
	if ok {
		oldText, newText, _ = strings.Cut(replacement, "=>")
	}
	return ToolInvocation{Call: tools.Call{
		ID:          "tool_" + runID + "_edit",
		Name:        "workspace.edit_file",
		DisplayName: "Edit file",
		Risk:        tools.RiskHigh,
		Arguments: map[string]any{
			"path":        strings.TrimSpace(path),
			"old_text":    strings.TrimSpace(oldText),
			"new_text":    strings.TrimSpace(newText),
			"replace_all": false,
		},
	}}
}

func diffInvocation(runID string, rest string) ToolInvocation {
	path, replacement, ok := strings.Cut(rest, " ")
	oldText, newText := "", ""
	if ok {
		oldText, newText, _ = strings.Cut(replacement, "=>")
	}
	return ToolInvocation{Call: tools.Call{
		ID:          "tool_" + runID + "_diff",
		Name:        "workspace.diff_file",
		DisplayName: "Preview diff",
		Risk:        tools.RiskLow,
		Arguments: map[string]any{
			"path":        strings.TrimSpace(path),
			"old_text":    strings.TrimSpace(oldText),
			"new_text":    strings.TrimSpace(newText),
			"replace_all": false,
		},
	}}
}

func patchInvocation(runID string, patch string) ToolInvocation {
	return ToolInvocation{Call: tools.Call{
		ID:          "tool_" + runID + "_patch",
		Name:        "workspace.apply_patch",
		DisplayName: "Apply patch",
		Risk:        tools.RiskHigh,
		Arguments:   map[string]any{"patch": patch},
	}}
}

func runReadFile(root string, relPath string) (string, error) {
	target, err := resolveWorkspacePath(root, relPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", relPath)
	}
	file, err := os.Open(target)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(file, maxToolOutputBytes)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func runListWorkspace(root string, relPath string, maxDepth int) (string, error) {
	if strings.TrimSpace(relPath) == "" {
		relPath = "."
	}
	maxDepth = clampInt(maxDepth, 0, 8)
	target, err := resolveWorkspacePath(root, relPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	cleanRoot, err := cleanWorkspaceRoot(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		display, err := workspaceRelativeDisplay(cleanRoot, target)
		if err != nil {
			return "", err
		}
		return display, nil
	}

	var entries []string
	var truncated bool
	err = filepath.WalkDir(target, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		relToTarget, err := filepath.Rel(target, path)
		if err != nil {
			return nil
		}
		depth := entryDepth(relToTarget)
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relToTarget != "." && d.IsDir() && shouldSkipSearchDir(d.Name()) {
			return filepath.SkipDir
		}
		display, err := workspaceRelativeDisplay(cleanRoot, path)
		if err != nil {
			return nil
		}
		if d.IsDir() {
			display += "/"
		}
		entries = append(entries, display)
		if len(entries) >= maxListEntries {
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return "empty", nil
	}
	output := strings.Join(entries, "\n")
	if truncated {
		output += "\n[truncated]"
	}
	return truncateToolOutput(output), nil
}

func runGrepWorkspace(root string, pattern string, relPath string, maxMatches int) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if strings.TrimSpace(relPath) == "" {
		relPath = "."
	}
	expr, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	maxMatches = clampInt(maxMatches, 1, 1000)
	target, err := resolveWorkspacePath(root, relPath)
	if err != nil {
		return "", err
	}
	cleanRoot, err := cleanWorkspaceRoot(root)
	if err != nil {
		return "", err
	}
	var matches []string
	var truncated bool
	visit := func(path string) error {
		if len(matches) >= maxMatches {
			truncated = true
			return filepath.SkipAll
		}
		lines, err := grepFile(cleanRoot, path, expr, maxMatches-len(matches))
		if err != nil {
			return nil
		}
		matches = append(matches, lines...)
		if len(matches) >= maxMatches {
			truncated = true
		}
		return nil
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		if err := visit(target); err != nil && err != filepath.SkipAll {
			return "", err
		}
	} else {
		err = filepath.WalkDir(target, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				if path != target && shouldSkipSearchDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			return visit(path)
		})
		if err != nil && err != filepath.SkipAll {
			return "", err
		}
	}
	if len(matches) == 0 {
		return "no matches", nil
	}
	output := strings.Join(matches, "\n")
	if truncated {
		output += "\n[truncated]"
	}
	return truncateToolOutput(output), nil
}

func grepFile(root string, path string, expr *regexp.Regexp, limit int) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() > maxGrepFileBytes {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	peek, _ := reader.Peek(4096)
	if bytes.IndexByte(peek, 0) >= 0 {
		return nil, nil
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var matches []string
	lineNo := 0
	display, err := workspaceRelativeDisplay(root, path)
	if err != nil {
		return nil, err
	}
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if expr.MatchString(line) {
			matches = append(matches, fmt.Sprintf("%s:%d: %s", display, lineNo, strings.TrimSpace(line)))
			if len(matches) >= limit {
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

func runWriteFile(root string, relPath string, content string) (string, error) {
	target, err := resolveWorkspacePath(root, relPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), relPath), nil
}

func runEditFile(root string, relPath string, oldText string, newText string, replaceAll bool) (string, error) {
	if strings.TrimSpace(oldText) == "" {
		return "", fmt.Errorf("old_text is required")
	}
	target, err := resolveWorkspacePath(root, relPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", relPath)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return "", fmt.Errorf("%s appears to be a binary file", relPath)
	}
	content := string(raw)
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", fmt.Errorf("old_text not found in %s", relPath)
	}
	if !replaceAll && count > 1 {
		return "", fmt.Errorf("old_text occurs %d times in %s; set replace_all to true to replace all matches", count, relPath)
	}
	replaced := strings.Replace(content, oldText, newText, 1)
	changed := 1
	if replaceAll {
		replaced = strings.ReplaceAll(content, oldText, newText)
		changed = count
	}
	if err := os.WriteFile(target, []byte(replaced), info.Mode().Perm()); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s: replaced %d occurrence(s)", relPath, changed), nil
}

func runDiffFile(root string, relPath string, content string, hasContent bool, oldText string, newText string, replaceAll bool) (string, error) {
	target, err := resolveWorkspacePath(root, relPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", relPath)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return "", fmt.Errorf("%s appears to be a binary file", relPath)
	}
	oldContent := string(raw)
	newContent := content
	if !hasContent {
		if strings.TrimSpace(oldText) == "" {
			return "", fmt.Errorf("old_text is required when content is not provided")
		}
		count := strings.Count(oldContent, oldText)
		if count == 0 {
			return "", fmt.Errorf("old_text not found in %s", relPath)
		}
		if !replaceAll && count > 1 {
			return "", fmt.Errorf("old_text occurs %d times in %s; set replace_all to true to preview all matches", count, relPath)
		}
		newContent = strings.Replace(oldContent, oldText, newText, 1)
		if replaceAll {
			newContent = strings.ReplaceAll(oldContent, oldText, newText)
		}
	}
	if oldContent == newContent {
		return "no changes", nil
	}
	return unifiedDiff(relPath, oldContent, newContent), nil
}

func runApplyPatch(root string, patch string) (string, error) {
	if strings.TrimSpace(patch) == "" {
		return "", fmt.Errorf("patch is required")
	}
	if len(patch) > maxPatchBytes {
		return "", fmt.Errorf("patch exceeds %d bytes", maxPatchBytes)
	}
	patch = strings.ReplaceAll(patch, "\r\n", "\n")
	patch = strings.ReplaceAll(patch, "\r", "\n")
	files, err := parseUnifiedPatch(patch)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("patch contains no file changes")
	}
	changed := make([]string, 0, len(files))
	for _, filePatch := range files {
		target, err := resolveWorkspacePath(root, filePatch.Path)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(target)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", filePatch.Path)
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			return "", err
		}
		if bytes.IndexByte(raw, 0) >= 0 {
			return "", fmt.Errorf("%s appears to be a binary file", filePatch.Path)
		}
		next, err := applyFilePatch(string(raw), filePatch)
		if err != nil {
			return "", fmt.Errorf("%s: %w", filePatch.Path, err)
		}
		if err := os.WriteFile(target, []byte(next), info.Mode().Perm()); err != nil {
			return "", err
		}
		changed = append(changed, filePatch.Path)
	}
	sort.Strings(changed)
	return fmt.Sprintf("applied patch to %d file(s): %s", len(changed), strings.Join(changed, ", ")), nil
}

func runShell(ctx context.Context, root string, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty shell command")
	}
	toolCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if goruntime.GOOS == "windows" {
		cmd = exec.CommandContext(toolCtx, "powershell", "-NoProfile", "-Command", command)
	} else {
		cmd = exec.CommandContext(toolCtx, "sh", "-c", command)
	}
	if root != "" {
		if resolved, err := filepath.Abs(root); err == nil {
			cmd.Dir = resolved
		}
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := truncateToolOutput(stdout.String() + stderr.String())
	if toolCtx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("shell command timed out")
	}
	if err != nil {
		return output, err
	}
	return output, nil
}

func resolveWorkspacePath(root string, relPath string) (string, error) {
	if strings.TrimSpace(relPath) == "" {
		return "", fmt.Errorf("path is required")
	}
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = wd
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		cleanRoot = filepath.Clean(absRoot)
	}
	target := relPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(cleanRoot, relPath)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	cleanTarget := filepath.Clean(absTarget)
	if evalTarget, err := filepath.EvalSymlinks(cleanTarget); err == nil {
		cleanTarget = filepath.Clean(evalTarget)
	}
	if !isPathInside(cleanRoot, cleanTarget) {
		return "", fmt.Errorf("path escapes workspace root")
	}
	return cleanTarget, nil
}

func cleanWorkspaceRoot(root string) (string, error) {
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = wd
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		cleanRoot = filepath.Clean(absRoot)
	}
	return filepath.Clean(cleanRoot), nil
}

func isPathInside(root string, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if goruntime.GOOS == "windows" {
		root = strings.ToLower(root)
		target = strings.ToLower(target)
	}
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(os.PathSeparator))
}

func truncateToolOutput(value string) string {
	if len(value) <= maxToolOutputBytes {
		return value
	}
	return value[:maxToolOutputBytes] + "\n[truncated]"
}

func workspaceRelativeDisplay(root string, target string) (string, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return ".", nil
	}
	return filepath.ToSlash(rel), nil
}

func entryDepth(rel string) int {
	if rel == "." || rel == "" {
		return 0
	}
	return len(strings.Split(filepath.Clean(rel), string(os.PathSeparator)))
}

func shouldSkipSearchDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", "bin", ".wails", ".vite":
		return true
	default:
		return false
	}
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func stringArgDefault(args map[string]any, key string, fallback string) string {
	value := strings.TrimSpace(stringArg(args, key))
	if value == "" {
		return fallback
	}
	return value
}

func intArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case jsonNumber:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func boolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func stringArgPresent(args map[string]any, key string) (string, bool) {
	value, ok := args[key].(string)
	return value, ok
}

type jsonNumber interface {
	Int64() (int64, error)
}

type unifiedFilePatch struct {
	Path  string
	Hunks []unifiedHunk
}

type unifiedHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string
}

func unifiedDiff(path string, oldContent string, newContent string) string {
	oldLines := splitContentLines(oldContent)
	newLines := splitContentLines(newContent)
	var builder strings.Builder
	display := filepath.ToSlash(strings.TrimSpace(path))
	fmt.Fprintf(&builder, "--- a/%s\n", display)
	fmt.Fprintf(&builder, "+++ b/%s\n", display)
	fmt.Fprintf(&builder, "@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines))
	for _, line := range oldLines {
		builder.WriteString("-")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	for _, line := range newLines {
		builder.WriteString("+")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	return truncateToolOutput(builder.String())
}

func parseUnifiedPatch(patch string) ([]unifiedFilePatch, error) {
	lines := strings.Split(patch, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var files []unifiedFilePatch
	for i := 0; i < len(lines); {
		if strings.HasPrefix(lines[i], "diff --git ") {
			i++
			continue
		}
		if isPatchMetadataLine(lines[i]) {
			i++
			continue
		}
		if !strings.HasPrefix(lines[i], "--- ") {
			return nil, fmt.Errorf("expected --- file header")
		}
		oldHeader := lines[i]
		i++
		if i >= len(lines) || !strings.HasPrefix(lines[i], "+++ ") {
			return nil, fmt.Errorf("expected +++ file header after %q", oldHeader)
		}
		path, err := patchPathFromHeader(lines[i])
		if err != nil {
			return nil, err
		}
		filePatch := unifiedFilePatch{Path: path}
		i++
		for i < len(lines) {
			if strings.HasPrefix(lines[i], "diff --git ") || strings.HasPrefix(lines[i], "--- ") {
				break
			}
			if isPatchMetadataLine(lines[i]) {
				i++
				continue
			}
			if !strings.HasPrefix(lines[i], "@@ ") {
				return nil, fmt.Errorf("expected hunk header for %s", path)
			}
			hunk, err := parseHunkHeader(lines[i])
			if err != nil {
				return nil, err
			}
			i++
			for i < len(lines) {
				line := lines[i]
				if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "@@ ") {
					break
				}
				if line == `\ No newline at end of file` {
					i++
					continue
				}
				if line == "" {
					return nil, fmt.Errorf("empty patch line in hunk for %s must be prefixed with context/add/remove marker", path)
				}
				switch line[0] {
				case ' ', '+', '-':
					hunk.Lines = append(hunk.Lines, line)
				default:
					return nil, fmt.Errorf("invalid hunk line marker %q for %s", line[0], path)
				}
				i++
			}
			filePatch.Hunks = append(filePatch.Hunks, hunk)
		}
		if len(filePatch.Hunks) == 0 {
			return nil, fmt.Errorf("patch for %s contains no hunks", path)
		}
		files = append(files, filePatch)
	}
	return files, nil
}

func isPatchMetadataLine(line string) bool {
	return strings.HasPrefix(line, "index ") ||
		strings.HasPrefix(line, "new file mode ") ||
		strings.HasPrefix(line, "deleted file mode ") ||
		strings.HasPrefix(line, "old mode ") ||
		strings.HasPrefix(line, "new mode ") ||
		strings.HasPrefix(line, "similarity index ") ||
		strings.HasPrefix(line, "rename from ") ||
		strings.HasPrefix(line, "rename to ")
}

func patchPathFromHeader(header string) (string, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(header, "+++ "))
	if raw == "/dev/null" {
		return "", fmt.Errorf("creating files from /dev/null patches is not supported")
	}
	if fields := strings.Fields(raw); len(fields) > 0 {
		raw = fields[0]
	}
	raw = strings.TrimPrefix(raw, "b/")
	raw = strings.TrimPrefix(raw, "a/")
	raw = strings.Trim(raw, `"`)
	if raw == "" || filepath.IsAbs(raw) || strings.HasPrefix(raw, "../") || strings.Contains(raw, "/../") || strings.Contains(raw, `\..\`) {
		return "", fmt.Errorf("invalid patch path %q", raw)
	}
	return filepath.ToSlash(raw), nil
}

func parseHunkHeader(header string) (unifiedHunk, error) {
	var h unifiedHunk
	if _, err := fmt.Sscanf(header, "@@ -%d,%d +%d,%d @@", &h.OldStart, &h.OldCount, &h.NewStart, &h.NewCount); err == nil {
		return h, nil
	}
	if _, err := fmt.Sscanf(header, "@@ -%d +%d @@", &h.OldStart, &h.NewStart); err == nil {
		h.OldCount = 1
		h.NewCount = 1
		return h, nil
	}
	if _, err := fmt.Sscanf(header, "@@ -%d,%d +%d @@", &h.OldStart, &h.OldCount, &h.NewStart); err == nil {
		h.NewCount = 1
		return h, nil
	}
	if _, err := fmt.Sscanf(header, "@@ -%d +%d,%d @@", &h.OldStart, &h.NewStart, &h.NewCount); err == nil {
		h.OldCount = 1
		return h, nil
	}
	return h, fmt.Errorf("invalid hunk header %q", header)
}

func applyFilePatch(content string, patch unifiedFilePatch) (string, error) {
	oldLines := splitContentLines(content)
	newLines := make([]string, 0, len(oldLines))
	oldIndex := 0
	for _, hunk := range patch.Hunks {
		targetIndex := hunk.OldStart - 1
		if hunk.OldStart == 0 {
			targetIndex = 0
		}
		if targetIndex < oldIndex || targetIndex > len(oldLines) {
			return "", fmt.Errorf("hunk starts outside file")
		}
		newLines = append(newLines, oldLines[oldIndex:targetIndex]...)
		oldIndex = targetIndex
		for _, line := range hunk.Lines {
			if line == "" {
				return "", fmt.Errorf("invalid empty hunk line")
			}
			text := line[1:]
			switch line[0] {
			case ' ':
				if oldIndex >= len(oldLines) || oldLines[oldIndex] != text {
					return "", fmt.Errorf("context mismatch at line %d", oldIndex+1)
				}
				newLines = append(newLines, oldLines[oldIndex])
				oldIndex++
			case '-':
				if oldIndex >= len(oldLines) || oldLines[oldIndex] != text {
					return "", fmt.Errorf("remove mismatch at line %d", oldIndex+1)
				}
				oldIndex++
			case '+':
				newLines = append(newLines, text)
			default:
				return "", fmt.Errorf("invalid hunk marker %q", line[0])
			}
		}
	}
	newLines = append(newLines, oldLines[oldIndex:]...)
	result := strings.Join(newLines, "\n")
	if strings.HasSuffix(content, "\n") && (len(newLines) > 0 || content == "\n") {
		result += "\n"
	}
	return result, nil
}

func splitContentLines(content string) []string {
	if content == "" {
		return nil
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
