package tools

import (
	"strings"

	ptools "redpanda/protocol/tools"
)

// Slash-command workspace tool builders (debug RED_PANDA_SLASH_TOOLS path).
func readInvocation(runID string, path string) ToolInvocation {
	return ToolInvocation{Call: ptools.Call{
		ID:          "tool_" + runID + "_read",
		Name:        "workspace.read_file",
		DisplayName: "Read file",
		Risk:        ptools.RiskLow,
		Arguments:   map[string]any{"path": path},
	}}
}

func listInvocation(runID string, path string) ToolInvocation {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	return ToolInvocation{Call: ptools.Call{
		ID:          "tool_" + runID + "_list",
		Name:        "workspace.list",
		DisplayName: "List files",
		Risk:        ptools.RiskLow,
		Arguments:   map[string]any{"path": path, "max_depth": defaultListDepth},
	}}
}

func grepInvocation(runID string, rest string) ToolInvocation {
	pattern, path, ok := strings.Cut(strings.TrimSpace(rest), " ")
	if !ok {
		path = "."
	}
	return ToolInvocation{Call: ptools.Call{
		ID:          "tool_" + runID + "_grep",
		Name:        "workspace.grep",
		DisplayName: "Search files",
		Risk:        ptools.RiskLow,
		Arguments:   map[string]any{"pattern": pattern, "path": strings.TrimSpace(path), "max_matches": defaultGrepMatches},
	}}
}

func shellInvocation(runID string, command string) ToolInvocation {
	return ToolInvocation{Call: ptools.Call{
		ID:          "tool_" + runID + "_shell",
		Name:        "shell.exec",
		DisplayName: "Shell",
		Risk:        ptools.RiskHigh,
		Arguments:   map[string]any{"command": command},
	}}
}

func writeInvocation(runID string, rest string) ToolInvocation {
	path, content, ok := strings.Cut(rest, " ")
	if !ok {
		path = rest
		content = ""
	}
	return ToolInvocation{Call: ptools.Call{
		ID:          "tool_" + runID + "_write",
		Name:        "workspace.write_file",
		DisplayName: "Write file",
		Risk:        ptools.RiskHigh,
		Arguments:   map[string]any{"path": path, "content": content},
	}}
}

func editInvocation(runID string, rest string) ToolInvocation {
	path, replacement, ok := strings.Cut(rest, " ")
	oldText, newText := "", ""
	if ok {
		oldText, newText, _ = strings.Cut(replacement, "=>")
	}
	return ToolInvocation{Call: ptools.Call{
		ID:          "tool_" + runID + "_edit",
		Name:        "workspace.edit_file",
		DisplayName: "Edit file",
		Risk:        ptools.RiskHigh,
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
	return ToolInvocation{Call: ptools.Call{
		ID:          "tool_" + runID + "_diff",
		Name:        "workspace.diff_file",
		DisplayName: "Preview diff",
		Risk:        ptools.RiskLow,
		Arguments: map[string]any{
			"path":        strings.TrimSpace(path),
			"old_text":    strings.TrimSpace(oldText),
			"new_text":    strings.TrimSpace(newText),
			"replace_all": false,
		},
	}}
}

func patchInvocation(runID string, patch string) ToolInvocation {
	return ToolInvocation{Call: ptools.Call{
		ID:          "tool_" + runID + "_patch",
		Name:        "workspace.apply_patch",
		DisplayName: "Apply patch",
		Risk:        ptools.RiskHigh,
		Arguments:   map[string]any{"patch": patch},
	}}
}
