package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"redpanda/agent/internal/pathutil"
	"strings"
)

// Workspace path resolution and traversal helpers.
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
