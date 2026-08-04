package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"redpanda/agent/internal/pathutil"
	"regexp"
	"sort"
	"strings"
)

// ---------- helpers copied from tools/helpers.go ----------

type jsonNumber interface {
	Int64() (int64, error)
}

// StringArg extracts a string argument.
func StringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

// StringArgDefault extracts a string argument, trimmed, with a fallback.
func StringArgDefault(args map[string]any, key string, fallback string) string {
	v := strings.TrimSpace(StringArg(args, key))
	if v == "" {
		return fallback
	}
	return v
}

// IntArg extracts an integer argument from int/int64/float64/jsonNumber.
func IntArg(args map[string]any, key string, fallback int) int {
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

// BoolArg extracts a boolean argument.
func BoolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

// StringArgPresent returns the string value and presence flag.
func StringArgPresent(args map[string]any, key string) (string, bool) {
	value, ok := args[key].(string)
	return value, ok
}

// StringListArg extracts a string slice from args.
func StringListArg(args map[string]any, key string) []string {
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

// TruncateToolOutput truncates output at maxToolOutputBytes.
func TruncateToolOutput(value string) string {
	if len(value) <= maxToolOutputBytes {
		return value
	}
	return value[:maxToolOutputBytes] + "\n[truncated]"
}

// clampInt clamps value to [min, max].
func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// stringDefault trims a string and returns fallback if empty.
func stringDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// ---------- workspace path helpers (copied from workspace_path.go) ----------

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

// resolveReadablePath permits absolute paths only inside the explicitly
// Gateway-owned read-only skill root. All other paths retain workspace-only
// sandboxing.
func resolveReadablePath(root string, relPath string, extraRoot string) (string, error) {
	if strings.TrimSpace(extraRoot) == "" || !filepath.IsAbs(strings.TrimSpace(relPath)) {
		return resolveWorkspacePath(root, relPath)
	}
	allowedRoot, err := filepath.Abs(extraRoot)
	if err != nil {
		return "", err
	}
	allowedRoot, err = filepath.EvalSymlinks(filepath.Clean(allowedRoot))
	if err != nil {
		return "", fmt.Errorf("Gateway skill directory is not accessible: %w", err)
	}
	target, err := filepath.Abs(relPath)
	if err != nil {
		return "", err
	}
	target = filepath.Clean(target)
	if resolved, evalErr := filepath.EvalSymlinks(target); evalErr == nil {
		target = filepath.Clean(resolved)
	}
	if !pathutil.IsPathInside(allowedRoot, target) {
		return "", fmt.Errorf("path escapes Gateway skill directory")
	}
	return target, nil
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

// ---------- find-files file-pattern matcher (copied from coding.go) ----------

func compileFilePattern(pattern string) (func(string, string) bool, error) {
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, `\`, "/"))
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
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

// ---------- find-files implementation (copied from coding.go runFindFiles) ----------

func RunFindFiles(root string, relPath string, pattern string, maxResults int) (string, error) {
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

// ---------- read-files implementation (copied from coding.go runReadFiles) ----------

func RunReadFiles(root string, paths []string) (string, error) {
	return RunReadFilesFromRoots(root, paths, "")
}

func RunReadFilesFromRoots(root string, paths []string, extraRoot string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("paths is required")
	}
	if len(paths) > DefaultReadFiles {
		return "", fmt.Errorf("paths exceeds %d files", DefaultReadFiles)
	}
	var output strings.Builder
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			return "", fmt.Errorf("paths contains an empty path")
		}
		content, err := RunReadFileFromRoots(root, p, extraRoot)
		if err != nil {
			return output.String(), fmt.Errorf("%s: %w", p, err)
		}
		if output.Len() > 0 {
			output.WriteString("\n\n")
		}
		fmt.Fprintf(&output, "--- %s ---\n%s", filepath.ToSlash(p), content)
		if output.Len() >= maxToolOutputBytes {
			return TruncateToolOutput(output.String()), nil
		}
	}
	return TruncateToolOutput(output.String()), nil
}
