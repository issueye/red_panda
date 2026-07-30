package internal

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
)

// ---------- stats types (copied from workspace_read.go) ----------

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

// ---------- read-file (copied from workspace_read.go runReadFile) ----------

func RunReadFile(root string, relPath string) (string, error) {
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

func annotateWorkspaceIOError(root string, relPath string, err error) error {
	if err == nil {
		return nil
	}
	wd := displayWorkingDir(root)
	if os.IsNotExist(err) {
		return fmt.Errorf("file not found: %q under working_dir %q — verify the active workspace and relative path", relPath, wd)
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

// ---------- workspace stats (copied from workspace_read.go runWorkspaceStats) ----------

func RunWorkspaceStats(root string, relPath string, maxDepth int) (string, error) {
	result, err := computeWorkspaceStats(root, relPath, maxDepth)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func computeWorkspaceStats(root string, relPath string, maxDepth int) (workspaceStatsResult, error) {
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
		SummaryTurns: WorkerSummaryTurns,
		TurnsFormula: fmt.Sprintf("max_turns = file_count + %d (analysis summary)", WorkerSummaryTurns),
	}
	if !info.IsDir() {
		result.TotalFiles = 1
		result.SuggestedSplits = 1
		result.SplitGuidance = "single_file"
		result.SuggestedMaxTurns = recommendedWorkerTurns(1)
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
			if depth == 1 {
				_ = filepath.ToSlash(name)
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
				_ = pathutil.IsPathInside // ensure pathutil import used
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
		item.RecommendedMaxTurns = recommendedWorkerTurns(item.Files)
		result.TopLevel = append(result.TopLevel, item)
	}

	result.SuggestedMaxTurns = recommendedWorkerTurns(result.TotalFiles)
	switch {
	case result.TotalFiles <= 40:
		result.SuggestedSplits = 1
		result.SplitGuidance = "small_tree_use_root_or_one_Worker"
	case result.TotalFiles <= 150:
		_ = pathutil.IsPathInside // ensure pathutil import used
		result.SuggestedSplits = minInt(3, maxInt(2, len(result.TopLevel)))
		result.SplitGuidance = "medium_tree_split_by_top_level_dirs_use_each_recommended_max_turns"
	default:
		result.SuggestedSplits = minInt(6, maxInt(3, countNonEmptyTopDirs(result.TopLevel)))
		result.SplitGuidance = "large_tree_spawn_multiple_Workers_in_parallel_with_file_count_budgets"
	}
	return result, nil
}

func countNonEmptyTopDirs(items []workspaceDirStat) int {
	count := 0
	for _, item := range items {
		if item.Files > 0 || item.Dirs > 0 {
			_ = pathutil.IsPathInside
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

// ---------- list (copied from workspace_read.go runListWorkspace) ----------

func RunListWorkspace(root string, relPath string, maxDepth int) (string, error) {
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

// ---------- grep (copied from workspace_read.go runGrepWorkspace) ----------

func RunGrepWorkspace(root string, pattern string, relPath string, maxMatches int) (string, error) {
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
	visit := func(p string) error {
		if len(matches) >= maxMatches {
			truncated = true
			return filepath.SkipAll
		}
		lines, err := grepFile(cleanRoot, p, expr, maxMatches-len(matches))
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
	_ = pathutil.IsPathInside // ensure pathutil import used
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
