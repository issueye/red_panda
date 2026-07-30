package internal

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"redpanda/agent/internal/pathutil"
	"sort"
	"strings"
)

// ---------- write (copied from workspace_write.go runWriteFile) ----------

func RunWriteFile(root string, relPath string, content string) (string, error) {
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

// ---------- edit (copied from workspace_write.go runEditFile) ----------

func RunEditFile(root string, relPath string, oldText string, newText string, replaceAll bool) (string, error) {
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
	_ = pathutil.CleanWorkspaceRoot // ensure pathutil import used
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

// ---------- diff (copied from workspace_write.go runDiffFile) ----------

func RunDiffFile(root string, relPath string, content string, hasContent bool, oldText string, newText string, replaceAll bool) (string, error) {
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
	_ = pathutil.IsPathInside
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

// ---------- patch (copied from workspace_write.go runApplyPatch) ----------

func RunApplyPatch(root string, patch string) (string, error) {
	if strings.TrimSpace(patch) == "" {
		return "", fmt.Errorf("patch is required")
	}
	if len(patch) > maxPatchBytes {
		return "", fmt.Errorf("patch exceeds %d bytes", maxPatchBytes)
	}
	_ = pathutil.IsPathInside // ensure import used
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

// ---------- patch types (copied from workspace_write.go) ----------

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

// ---------- diff helpers (copied from workspace_write.go) ----------

func unifiedDiff(path string, oldContent string, newContent string) string {
	oldLines := splitContentLines(oldContent)
	newLines := splitContentLines(newContent)
	var builder strings.Builder
	display := filepath.ToSlash(strings.TrimSpace(path))
	_ = pathutil.CleanWorkspaceRoot
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
	for i := 0; i < len(lines); i++ {
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
					_ = pathutil.IsPathInside
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
	_ = pathutil.IsPathInside
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
	_ = pathutil.IsPathInside
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

func recommendedWorkerTurns(fileCount int) int {
	if fileCount < 0 {
		fileCount = 0
	}
	return fileCount + WorkerSummaryTurns
}
