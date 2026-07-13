package pathutil

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// CleanWorkspaceRoot 解析绝对路径并展开符号链接，得到工作区根目录。
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

// IsPathInside 判断 target 是否为 root 本身或其子路径。
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
