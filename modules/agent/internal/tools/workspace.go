package tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"redpanda/agent/internal/pathutil"
	"regexp"
	"sort"
	"strings"

	ptools "redpanda/protocol/tools"
)

const maxGrepFileBytes = 2 * 1024 * 1024
const defaultListDepth = 3
const subagentSummaryTurns = 8
const maxListEntries = 500
const defaultGrepMatches = 100
const maxPatchBytes = 256 * 1024

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

func runReadFile(root string, relPath string) (string, error) {
	target, err := resolveWorkspacePath(root, relPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", annotateWorkspaceIOError(root, relPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory (working_dir=%q)", relPath, displayWorkingDir(root))
	}
	file, err := os.Open(target)
	if err != nil {
		return "", annotateWorkspaceIOError(root, relPath, err)
	}
	defer file.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(file, maxToolOutputBytes)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// annotateWorkspaceIOError 将难以理解的操作系统错误转换为模型可操作的提示，
// 尤其是“错误 working_dir 下找不到文件”的情况。
func annotateWorkspaceIOError(root string, relPath string, err error) error {
	if err == nil {
		return nil
	}
	wd := displayWorkingDir(root)
	if os.IsNotExist(err) {
		return fmt.Errorf("file not found: %q under working_dir %q 鈥?verify the active workspace and relative path", relPath, wd)
	}
	return fmt.Errorf("%w (path=%q working_dir=%q)", err, relPath, wd)
}

func displayWorkingDir(root string) string {
	if strings.TrimSpace(root) == "" {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return "(unset)"
	}
	return root
}

type workspaceDirStat struct {
	Path                string `json:"path"`
	Files               int    `json:"files"`
	Dirs                int    `json:"dirs"`
	RecommendedMaxTurns int    `json:"recommended_max_turns"`
	IsSkipped           bool   `json:"skipped,omitempty"`
}

type workspaceStatsResult struct {
	Root              string             `json:"root"`
	Path              string             `json:"path"`
	MaxDepth          int                `json:"max_depth"`
	TotalFiles        int                `json:"total_files"`
	TotalDirs         int                `json:"total_dirs"`
	Truncated         bool               `json:"truncated"`
	SuggestedSplits   int                `json:"suggested_splits"`
	SplitGuidance     string             `json:"split_guidance"`
	SummaryTurns      int                `json:"summary_turns"`
	SuggestedMaxTurns int                `json:"suggested_max_turns"`
	TurnsFormula      string             `json:"turns_formula"`
	TopLevel          []workspaceDirStat `json:"top_level"`
}

// runWorkspaceStats 遍历目录并返回用于拆分规划的统计数量。
func runWorkspaceStats(root string, relPath string, maxDepth int) (string, error) {
	result, err := ComputeWorkspaceStats(root, relPath, maxDepth)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// computeWorkspaceStats 返回供 workspace.stats 和 subagent.run 预算使用的结构化统计值。
func ComputeWorkspaceStats(root string, relPath string, maxDepth int) (workspaceStatsResult, error) {
	if strings.TrimSpace(relPath) == "" {
		relPath = "."
	}
	maxDepth = clampInt(maxDepth, 1, 8)
	target, err := resolveWorkspacePath(root, relPath)
	if err != nil {
		return workspaceStatsResult{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return workspaceStatsResult{}, err
	}
	cleanRoot, err := pathutil.CleanWorkspaceRoot(root)
	if err != nil {
		return workspaceStatsResult{}, err
	}
	displayRoot, err := workspaceRelativeDisplay(cleanRoot, target)
	if err != nil {
		displayRoot = relPath
	}

	result := workspaceStatsResult{
		Root:         cleanRoot,
		Path:         displayRoot,
		MaxDepth:     maxDepth,
		SummaryTurns: subagentSummaryTurns,
		TurnsFormula: fmt.Sprintf("max_turns = file_count + %d (analysis summary)", subagentSummaryTurns),
	}
	if !info.IsDir() {
		result.TotalFiles = 1
		result.SuggestedSplits = 1
		result.SplitGuidance = "single_file"
		result.SuggestedMaxTurns = recommendedSubagentTurns(1)
		return result, nil
	}

	const maxWalkFiles = 20000
	topLevel := map[string]*workspaceDirStat{}
	err = filepath.WalkDir(target, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		relToTarget, err := filepath.Rel(target, path)
		if err != nil {
			return nil
		}
		if relToTarget == "." {
			return nil
		}
		depth := entryDepth(relToTarget)
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if d.IsDir() && shouldSkipSearchDir(name) {
			// 仍统计被跳过的顶级包，便于编排器分配任务。
			if depth == 1 {
				key := filepath.ToSlash(name)
				stat := topLevel[key]
				if stat == nil {
					stat = &workspaceDirStat{Path: key + "/", IsSkipped: true}
					topLevel[key] = stat
				}
			}
			return filepath.SkipDir
		}

		topName := strings.Split(filepath.ToSlash(relToTarget), "/")[0]
		stat := topLevel[topName]
		if stat == nil {
			display := topName
			if d.IsDir() && depth == 1 {
				display = topName + "/"
			}
			stat = &workspaceDirStat{Path: display}
			topLevel[topName] = stat
		}

		if d.IsDir() {
			result.TotalDirs++
			if depth == 1 {
				stat.Dirs++
			}
			return nil
		}

		result.TotalFiles++
		stat.Files++
		if result.TotalFiles >= maxWalkFiles {
			result.Truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return workspaceStatsResult{}, err
	}

	keys := make([]string, 0, len(topLevel))
	for key := range topLevel {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := *topLevel[key]
		item.RecommendedMaxTurns = recommendedSubagentTurns(item.Files)
		result.TopLevel = append(result.TopLevel, item)
	}

	result.SuggestedMaxTurns = recommendedSubagentTurns(result.TotalFiles)
	switch {
	case result.TotalFiles <= 40:
		result.SuggestedSplits = 1
		result.SplitGuidance = "small_tree_use_root_or_one_subagent"
	case result.TotalFiles <= 150:
		result.SuggestedSplits = minInt(3, maxInt(2, len(result.TopLevel)))
		result.SplitGuidance = "medium_tree_split_by_top_level_dirs_use_each_recommended_max_turns"
	default:
		result.SuggestedSplits = minInt(6, maxInt(3, countNonEmptyTopDirs(result.TopLevel)))
		result.SplitGuidance = "large_tree_spawn_multiple_subagents_in_parallel_with_file_count_budgets"
	}
	return result, nil
}

func countNonEmptyTopDirs(items []workspaceDirStat) int {
	count := 0
	for _, item := range items {
		if item.Files > 0 || item.Dirs > 0 {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
	cleanRoot, err := pathutil.CleanWorkspaceRoot(root)
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
	return TruncateToolOutput(output), nil
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
	cleanRoot, err := pathutil.CleanWorkspaceRoot(root)
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
	return TruncateToolOutput(output), nil
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
	// 会话 working_dir 不存在或错误时快速失败；否则每次相对路径读取都会像文件缺失，
	// 从而误导模型。
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
	return TruncateToolOutput(builder.String())
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

func recommendedSubagentTurns(fileCount int) int {
	if fileCount < 0 {
		fileCount = 0
	}
	return fileCount + subagentSummaryTurns
}
