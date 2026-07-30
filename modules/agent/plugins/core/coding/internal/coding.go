package internal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"redpanda/agent/internal/pathutil"
	"strconv"
	"strings"

	ptools "redpanda/protocol/tools"
)

// WrapResult converts (output, error) into a *ptools.Result.
func WrapResult(name string, output string, err error) (*ptools.Result, error) {
	if err != nil {
		return &ptools.Result{
			Name:   name,
			Status: ptools.CallStatusFailed,
			Output: output,
			Error:  err.Error(),
		}, err
	}
	return &ptools.Result{
		Name:   name,
		Status: ptools.CallStatusCompleted,
		Output: output,
	}, nil
}

// ---------------------------------------------------------------------------
// Git command helpers — function bodies identical to tools/coding.go.
// ---------------------------------------------------------------------------

func runGit(ctx context.Context, root string, args ...string) (string, error) {
	cleanRoot, err := pathutil.CleanWorkspaceRoot(root)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cleanRoot
	cmd.Stdin = bytes.NewReader(nil)
	output, err := cmd.CombinedOutput()
	text := TruncateToolOutput(strings.TrimSpace(string(output)))
	if err != nil {
		return text, err
	}
	if text == "" {
		return "no output", nil
	}
	return text, nil
}

func RunGitStatus(ctx context.Context, root string) (string, error) {
	return runGit(ctx, root, "status", "--short", "--branch")
}

func RunGitDiff(ctx context.Context, root string, staged bool, revision string, path string) (string, error) {
	args := []string{"diff", "--no-ext-diff", "--no-color"}
	if staged {
		args = append(args, "--cached")
	}
	if revision = strings.TrimSpace(revision); revision != "" {
		if strings.HasPrefix(revision, "-") {
			return "", fmt.Errorf("revision cannot start with '-'")
		}
		args = append(args, revision)
	}
	if path = strings.TrimSpace(path); path != "" {
		if _, err := resolveWorkspacePath(root, path); err != nil {
			return "", err
		}
		args = append(args, "--", filepath.ToSlash(path))
	}
	return runGit(ctx, root, args...)
}

func RunGitLog(ctx context.Context, root string, maxCount int, path string) (string, error) {
	maxCount = clampInt(maxCount, 1, 50)
	args := []string{"log", "--no-color", "--date=short", "--pretty=format:%h%x09%ad%x09%s", "-n", strconv.Itoa(maxCount)}
	if path = strings.TrimSpace(path); path != "" {
		if _, err := resolveWorkspacePath(root, path); err != nil {
			return "", err
		}
		args = append(args, "--", filepath.ToSlash(path))
	}
	return runGit(ctx, root, args...)
}

func RunGitShow(ctx context.Context, root string, revision string, path string) (string, error) {
	revision = stringDefault(strings.TrimSpace(revision), "HEAD")
	if strings.HasPrefix(revision, "-") {
		return "", fmt.Errorf("revision cannot start with '-'")
	}
	args := []string{"show", "--no-ext-diff", "--no-color", "--format=fuller", "--stat", "--patch", revision}
	if path = strings.TrimSpace(path); path != "" {
		if _, err := resolveWorkspacePath(root, path); err != nil {
			return "", err
		}
		args = append(args, "--", filepath.ToSlash(path))
	}
	return runGit(ctx, root, args...)
}

// ---------------------------------------------------------------------------
// Workspace path resolution — identical to tools/workspace_path.go.
// ---------------------------------------------------------------------------

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
	if info, statErr := os.Stat(absRoot); statErr != nil {
		if os.IsNotExist(statErr) {
			return "", fmt.Errorf("working_dir does not exist: %q", absRoot)
		}
		return "", fmt.Errorf("working_dir not accessible: %q: %w", absRoot, statErr)
	} else if !info.IsDir() {
		return "", fmt.Errorf("working_dir is not a directory: %q", absRoot)
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
	if !pathutil.IsPathInside(cleanRoot, cleanTarget) {
		return "", fmt.Errorf("path escapes workspace root")
	}
	return cleanTarget, nil
}

// ---------------------------------------------------------------------------
// Shared helpers — identical to tools/helpers.go (subset needed by git tools).
// ---------------------------------------------------------------------------

const maxToolOutputBytes = 64 * 1024

func TruncateToolOutput(value string) string {
	if len(value) <= maxToolOutputBytes {
		return value
	}
	return value[:maxToolOutputBytes] + "\n[truncated]"
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

func stringDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// StrArg extracts a string value from args.
func StrArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

// IntArg extracts an int value from args with a fallback.
func IntArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return fallback
}

// BoolArg extracts a bool value from args with a fallback.
func BoolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key].(bool)
	if !ok {
		return fallback
	}
	return value
}
