package pathutil

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// CleanWorkspaceRoot resolves an absolute, symlink-evaluated workspace root.
func CleanWorkspaceRoot(root string) (string, error) {
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
	cleanRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		cleanRoot = filepath.Clean(absRoot)
	}
	return filepath.Clean(cleanRoot), nil
}

// IsPathInside reports whether target is root or a descendant of root.
func IsPathInside(root string, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if goruntime.GOOS == "windows" {
		root = strings.ToLower(root)
		target = strings.ToLower(target)
	}
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(os.PathSeparator))
}
