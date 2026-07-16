package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"redpanda/mcpkit"
)

func registerTools(s *server.MCPServer) {
	mcpkit.AddTextTool(s, mcpkit.NewTool("version",
		mcpkit.WithDescription("Return robotgo-flow CLI version and resolved executable path."),
	), func(ctx context.Context, args map[string]any) (string, bool) {
		return toolVersion()
	})

	mcpkit.AddTextTool(s, mcpkit.NewTool("list_workflows",
		mcpkit.WithDescription("List YAML workflow files under a directory (non-recursive by default). "+
			"Use before run_workflow to discover available RPA flows."),
		mcpkit.WithString("directory",
			mcpkit.Description("Directory to scan. Defaults to ROBOTGO_FLOW_WORKFLOWS or cwd."),
		),
		mcpkit.WithBoolean("recursive",
			mcpkit.Description("When true, walk subdirectories for *.yml / *.yaml."),
		),
	), func(ctx context.Context, args map[string]any) (string, bool) {
		return toolListWorkflows(args)
	})

	mcpkit.AddTextTool(s, mcpkit.NewTool("inspect_workflow",
		mcpkit.WithDescription("Read a robotgo-flow YAML workflow and return a structural summary "+
			"(name, inputs, step count, action types). Does not execute desktop automation."),
		mcpkit.WithString("path",
			mcpkit.Required(),
			mcpkit.Description("Path to workflow YAML file."),
		),
	), func(ctx context.Context, args map[string]any) (string, bool) {
		return toolInspectWorkflow(args)
	})

	mcpkit.AddTextTool(s, mcpkit.NewTool("run_workflow",
		mcpkit.WithDescription("Execute a robotgo-flow desktop RPA workflow via the CLI. "+
			"HIGH RISK: drives real mouse/keyboard on the host Windows desktop. "+
			"Requires robotgo-flow.exe (ROBOTGO_FLOW_COMMAND). "+
			"Optional inputs map supplies $input.* placeholders non-interactively via env ROBOTGO_FLOW_INPUTS_JSON when supported, "+
			"or is returned as required-inputs preview if the workflow needs interactive input."),
		mcpkit.WithString("path",
			mcpkit.Required(),
			mcpkit.Description("Path to workflow YAML."),
		),
		mcpkit.WithNumber("from_step",
			mcpkit.Description("1-indexed step to start from (robotgo-flow --from)."),
		),
		mcpkit.WithBoolean("debug",
			mcpkit.Description("Enable --debug screenshots after each step."),
		),
		mcpkit.WithString("out_dir",
			mcpkit.Description("Screenshot output directory (--out)."),
		),
		mcpkit.WithObject("inputs",
			mcpkit.Description("Runtime input values keyed by input name (for $input.*)."),
		),
		mcpkit.WithNumber("timeout_sec",
			mcpkit.Description("Hard timeout for the CLI process (default 600)."),
		),
		mcpkit.WithBoolean("dry_run",
			mcpkit.Description("If true, only inspect the workflow and print the planned CLI command without executing."),
		),
	), func(ctx context.Context, args map[string]any) (string, bool) {
		return toolRunWorkflow(args)
	})
}

func toolVersion() (string, bool) {
	cmdPath, err := resolveRobotgoCommand()
	if err != nil {
		return err.Error(), true
	}
	out, err := exec.Command(cmdPath, "version").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("command=%s\nerror=%v\noutput=%s", cmdPath, err, string(out)), true
	}
	return fmt.Sprintf("command=%s\n%s", cmdPath, strings.TrimSpace(string(out))), false
}

func toolListWorkflows(args map[string]any) (string, bool) {
	dir := stringArg(args, "directory")
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("ROBOTGO_FLOW_WORKFLOWS"))
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err.Error(), true
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err.Error(), true
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("directory not accessible: %v", err), true
	}
	if !info.IsDir() {
		return "path is not a directory", true
	}

	recursive := boolArg(args, "recursive", false)
	var files []string
	if recursive {
		_ = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if isYAML(path) {
				files = append(files, path)
			}
			return nil
		})
	} else {
		entries, err := os.ReadDir(abs)
		if err != nil {
			return err.Error(), true
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(abs, e.Name())
			if isYAML(p) {
				files = append(files, p)
			}
		}
	}
	sort.Strings(files)
	var b strings.Builder
	fmt.Fprintf(&b, "directory=%s recursive=%v count=%d\n", abs, recursive, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(abs, f)
		if err != nil {
			rel = f
		}
		fmt.Fprintf(&b, "- %s\n", filepath.ToSlash(rel))
	}
	return strings.TrimSpace(b.String()), false
}

func toolInspectWorkflow(args map[string]any) (string, bool) {
	path := stringArg(args, "path")
	if path == "" {
		return "path is required", true
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err.Error(), true
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("read failed: %v", err), true
	}
	summary, err := summarizeWorkflowYAML(raw)
	if err != nil {
		return fmt.Sprintf("path=%s\nparse warning: %v\nraw_bytes=%d", abs, err, len(raw)), true
	}
	summary["path"] = filepath.ToSlash(abs)
	out, _ := json.MarshalIndent(summary, "", "  ")
	return string(out), false
}

func toolRunWorkflow(args map[string]any) (string, bool) {
	path := stringArg(args, "path")
	if path == "" {
		return "path is required", true
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err.Error(), true
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Sprintf("workflow not found: %v", err), true
	}

	// Always inspect first so the model sees structure / required inputs.
	raw, _ := os.ReadFile(abs)
	summary, _ := summarizeWorkflowYAML(raw)

	cmdPath, err := resolveRobotgoCommand()
	if err != nil {
		return err.Error(), true
	}

	cliArgs := []string{"run", abs}
	if from := intArg(args, "from_step", 0); from > 0 {
		cliArgs = append(cliArgs, "--from", fmt.Sprintf("%d", from))
	}
	if boolArg(args, "debug", false) {
		cliArgs = append(cliArgs, "--debug")
	}
	if outDir := stringArg(args, "out_dir"); outDir != "" {
		cliArgs = append(cliArgs, "--out", outDir)
	}

	if boolArg(args, "dry_run", false) {
		payload := map[string]any{
			"dry_run":  true,
			"command":  cmdPath,
			"args":     cliArgs,
			"workflow": summary,
		}
		out, _ := json.MarshalIndent(payload, "", "  ")
		return string(out), false
	}

	// Warn if workflow declares inputs but none provided (CLI would block on stdin).
	inputs := mapStringArg(args, "inputs")
	if summary != nil {
		if required, ok := summary["required_inputs"].([]string); ok && len(required) > 0 && len(inputs) == 0 {
			return fmt.Sprintf(
				"workflow requires inputs %v but none were provided.\n"+
					"Re-call run_workflow with arguments.inputs map, or dry_run=true to only inspect.\n"+
					"workflow=%s",
				required, filepath.ToSlash(abs),
			), true
		}
	}

	timeoutSec := intArg(args, "timeout_sec", 600)
	if timeoutSec < 5 {
		timeoutSec = 5
	}

	cmd := exec.Command(cmdPath, cliArgs...)
	cmd.Dir = filepath.Dir(abs)
	env := os.Environ()
	if len(inputs) > 0 {
		// Hint for future CLI non-interactive mode; current robotgo-flow may still prompt.
		blob, _ := json.Marshal(inputs)
		env = append(env, "ROBOTGO_FLOW_INPUTS_JSON="+string(blob))
	}
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("start failed: %v", err), true
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		out := strings.TrimSpace(stdout.String())
		errOut := strings.TrimSpace(stderr.String())
		var b strings.Builder
		fmt.Fprintf(&b, "command=%s %s\nexit_error=%v\n", cmdPath, strings.Join(cliArgs, " "), err)
		if out != "" {
			fmt.Fprintf(&b, "--- stdout ---\n%s\n", truncate(out, 48*1024))
		}
		if errOut != "" {
			fmt.Fprintf(&b, "--- stderr ---\n%s\n", truncate(errOut, 16*1024))
		}
		return strings.TrimSpace(b.String()), err != nil
	case <-timer.C:
		_ = cmd.Process.Kill()
		return fmt.Sprintf("workflow timed out after %ds; process killed", timeoutSec), true
	}
}

func resolveRobotgoCommand() (string, error) {
	candidates := []string{
		strings.TrimSpace(os.Getenv("ROBOTGO_FLOW_COMMAND")),
		"robotgo-flow.exe",
		"robotgo-flow",
	}
	// Relative to this MCP binary: ../../../robotgo-flow/build...
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "robotgo-flow.exe"),
			filepath.Join(dir, "..", "robotgo-flow.exe"),
			filepath.Join(dir, "..", "..", "robotgo-flow", "robotgo-flow.exe"),
		)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if path, err := exec.LookPath(c); err == nil {
			return path, nil
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs, nil
		}
	}
	return "", fmt.Errorf("robotgo-flow executable not found; set ROBOTGO_FLOW_COMMAND to the full path of robotgo-flow.exe")
}

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml"
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func boolArg(args map[string]any, key string, def bool) bool {
	v, ok := args[key].(bool)
	if !ok {
		return def
	}
	return v
}

func intArg(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return def
	}
}

func mapStringArg(args map[string]any, key string) map[string]string {
	raw, ok := args[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch t := v.(type) {
		case string:
			out[k] = t
		default:
			out[k] = fmt.Sprint(t)
		}
	}
	return out
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…[truncated]"
}
