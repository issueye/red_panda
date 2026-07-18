package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"redpanda/agent/internal/pathutil"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const defaultFindFiles = 200
const defaultReadFiles = 12

func runFindFiles(root string, relPath string, pattern string, maxResults int) (string, error) {
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, `\`, "/"))
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	target, err := resolveWorkspacePath(root, stringDefault(relPath, "."))
	if err != nil {
		return "", err
	}
	cleanRoot, err := pathutil.CleanWorkspaceRoot(root)
	if err != nil {
		return "", err
	}
	matcher, err := compileFilePattern(pattern)
	if err != nil {
		return "", err
	}
	maxResults = clampInt(maxResults, 1, 1000)

	var matches []string
	truncated := false
	err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != target && shouldSkipSearchDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := workspaceRelativeDisplay(cleanRoot, path)
		if relErr != nil || !matcher(rel, entry.Name()) {
			return nil
		}
		matches = append(matches, rel)
		if len(matches) >= maxResults {
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "no matches", nil
	}
	output := strings.Join(matches, "\n")
	if truncated {
		output += "\n[truncated]"
	}
	return TruncateToolOutput(output), nil
}

func compileFilePattern(pattern string) (func(string, string) bool, error) {
	if !strings.ContainsAny(pattern, "*?") {
		needle := strings.ToLower(pattern)
		return func(rel string, name string) bool {
			return strings.Contains(strings.ToLower(rel), needle) || strings.Contains(strings.ToLower(name), needle)
		}, nil
	}
	var expression strings.Builder
	expression.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					expression.WriteString("(?:.*/)?")
					i += 2
				} else {
					expression.WriteString(".*")
					i++
				}
			} else {
				expression.WriteString("[^/]*")
			}
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	expression.WriteString("$")
	compiled, err := regexp.Compile("(?i)" + expression.String())
	if err != nil {
		return nil, fmt.Errorf("invalid file pattern: %w", err)
	}
	return func(rel string, name string) bool {
		return compiled.MatchString(rel) || compiled.MatchString(name)
	}, nil
}

func runReadFiles(root string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("paths is required")
	}
	if len(paths) > defaultReadFiles {
		return "", fmt.Errorf("paths exceeds %d files", defaultReadFiles)
	}
	var output strings.Builder
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return "", fmt.Errorf("paths contains an empty path")
		}
		content, err := runReadFile(root, path)
		if err != nil {
			return output.String(), fmt.Errorf("%s: %w", path, err)
		}
		if output.Len() > 0 {
			output.WriteString("\n\n")
		}
		fmt.Fprintf(&output, "--- %s ---\n%s", filepath.ToSlash(path), content)
		if output.Len() >= maxToolOutputBytes {
			return TruncateToolOutput(output.String()), nil
		}
	}
	return TruncateToolOutput(output.String()), nil
}

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

func runGitStatus(ctx context.Context, root string) (string, error) {
	return runGit(ctx, root, "status", "--short", "--branch")
}

func runGitDiff(ctx context.Context, root string, staged bool, revision string, path string) (string, error) {
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

func runGitLog(ctx context.Context, root string, maxCount int, path string) (string, error) {
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

func runGitShow(ctx context.Context, root string, revision string, path string) (string, error) {
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

func stringDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stringListArg(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		items := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}
