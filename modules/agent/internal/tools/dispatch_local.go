package tools

import (
	"context"

	ptools "redpanda/protocol/tools"
)

// dispatchLocalTool handles workspace.*, git.*, and shell.exec tools.
// Returns handled=false when the name is not in this group.
func dispatchLocalTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (output string, handled bool, err error) {
	switch call.Name {
	case "workspace.read_file":
		out, e := runReadFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"))
		return out, true, e
	case "workspace.list":
		out, e := runListWorkspace(runCtx.WorkingDir, StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_depth", defaultListDepth))
		return out, true, e
	case "workspace.stats":
		out, e := runWorkspaceStats(runCtx.WorkingDir, StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_depth", 4))
		return out, true, e
	case "workspace.grep":
		out, e := runGrepWorkspace(runCtx.WorkingDir, StringArg(call.Arguments, "pattern"), StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_matches", defaultGrepMatches))
		return out, true, e
	case "workspace.find_files":
		out, e := runFindFiles(runCtx.WorkingDir, StringArgDefault(call.Arguments, "path", "."), StringArg(call.Arguments, "pattern"), IntArg(call.Arguments, "max_results", defaultFindFiles))
		return out, true, e
	case "workspace.read_files":
		out, e := runReadFiles(runCtx.WorkingDir, stringListArg(call.Arguments, "paths"))
		return out, true, e
	case "workspace.write_file":
		out, e := runWriteFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), StringArg(call.Arguments, "content"))
		return out, true, e
	case "workspace.edit_file":
		out, e := runEditFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), StringArg(call.Arguments, "old_text"), StringArg(call.Arguments, "new_text"), BoolArg(call.Arguments, "replace_all", false))
		return out, true, e
	case "workspace.diff_file":
		content, hasContent := stringArgPresent(call.Arguments, "content")
		out, e := runDiffFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), content, hasContent, StringArg(call.Arguments, "old_text"), StringArg(call.Arguments, "new_text"), BoolArg(call.Arguments, "replace_all", false))
		return out, true, e
	case "workspace.apply_patch":
		out, e := runApplyPatch(runCtx.WorkingDir, StringArg(call.Arguments, "patch"))
		return out, true, e
	case "git.status":
		out, e := runGitStatus(ctx, runCtx.WorkingDir)
		return out, true, e
	case "git.diff":
		out, e := runGitDiff(ctx, runCtx.WorkingDir, BoolArg(call.Arguments, "staged", false), StringArg(call.Arguments, "revision"), StringArg(call.Arguments, "path"))
		return out, true, e
	case "git.log":
		out, e := runGitLog(ctx, runCtx.WorkingDir, IntArg(call.Arguments, "max_count", 10), StringArg(call.Arguments, "path"))
		return out, true, e
	case "git.show":
		out, e := runGitShow(ctx, runCtx.WorkingDir, StringArg(call.Arguments, "revision"), StringArg(call.Arguments, "path"))
		return out, true, e
	case "shell.exec":
		out, e := runShell(ctx, runCtx.WorkingDir, StringArg(call.Arguments, "command"))
		return out, true, e
	default:
		return "", false, nil
	}
}
