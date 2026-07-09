package service

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultTreeDepth = 2
	maxTreeDepth     = 6
	maxTreeEntries   = 1000
	maxFileBytes     = 1024 * 1024
)

type TreeOptions struct {
	Root          string
	Path          string
	MaxDepth      int
	IncludeHidden bool
}

type TreeNodeDTO struct {
	Path       string        `json:"path"`
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Size       int64         `json:"size,omitempty"`
	ModifiedAt time.Time     `json:"modified_at"`
	Children   []TreeNodeDTO `json:"children,omitempty"`
}

type FileDTO struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	Content    string    `json:"content,omitempty"`
	Binary     bool      `json:"binary"`
	Truncated  bool      `json:"truncated"`
}

type DiffDTO struct {
	Available bool   `json:"available"`
	Root      string `json:"root"`
	Path      string `json:"path,omitempty"`
	Diff      string `json:"diff"`
	Reason    string `json:"reason,omitempty"`
}

func (s WorkspaceService) Tree(options TreeOptions) (TreeNodeDTO, error) {
	root, target, err := s.resolvePath(options.Root, options.Path)
	if err != nil {
		return TreeNodeDTO{}, err
	}
	depth := options.MaxDepth
	if depth <= 0 {
		depth = defaultTreeDepth
	}
	if depth > maxTreeDepth {
		depth = maxTreeDepth
	}
	counter := 0
	return s.buildTree(root, target, depth, options.IncludeHidden, &counter)
}

func (s WorkspaceService) File(rootValue string, relPath string) (FileDTO, error) {
	root, target, err := s.resolvePath(rootValue, relPath)
	if err != nil {
		return FileDTO{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return FileDTO{}, err
	}
	if info.IsDir() {
		return FileDTO{}, errors.New("path is a directory")
	}

	file, err := os.Open(target)
	if err != nil {
		return FileDTO{}, err
	}
	defer file.Close()

	limit := maxFileBytes + 1
	buf := make([]byte, limit)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return FileDTO{}, err
	}
	data := buf[:n]
	truncated := len(data) > maxFileBytes
	if truncated {
		data = data[:maxFileBytes]
	}
	binary := bytes.Contains(data, []byte{0}) || !utf8.Valid(data)
	content := ""
	if !binary {
		content = string(data)
	}
	rel, _ := filepath.Rel(root, target)
	return FileDTO{
		Path:       filepath.ToSlash(rel),
		Name:       info.Name(),
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC(),
		Content:    content,
		Binary:     binary,
		Truncated:  truncated,
	}, nil
}

func (s WorkspaceService) Diff(rootValue string, relPath string) (DiffDTO, error) {
	root, target, err := s.resolvePath(rootValue, relPath)
	if err != nil {
		return DiffDTO{}, err
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return DiffDTO{Available: false, Root: root, Path: filepath.ToSlash(relPath), Reason: "workspace is not a git repository"}, nil
	}

	args := []string{"-C", root, "diff", "--"}
	if relPath != "" {
		rel, _ := filepath.Rel(root, target)
		args = append(args, filepath.ToSlash(rel))
	}
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return DiffDTO{Available: false, Root: root, Path: filepath.ToSlash(relPath), Reason: strings.TrimSpace(string(output))}, nil
	}
	return DiffDTO{
		Available: true,
		Root:      root,
		Path:      filepath.ToSlash(relPath),
		Diff:      string(output),
	}, nil
}

func (s WorkspaceService) resolvePath(rootValue string, relPath string) (string, string, error) {
	root := rootValue
	if root == "" {
		current, err := s.Current()
		if err != nil {
			return "", "", err
		}
		if current == nil {
			return "", "", errors.New("workspace is not open")
		}
		root = current.RootPath
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	target := absRoot
	if relPath != "" {
		cleanRel := filepath.Clean(relPath)
		if filepath.IsAbs(cleanRel) {
			return "", "", errors.New("path must be relative")
		}
		target = filepath.Join(absRoot, cleanRel)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("path escapes workspace root")
	}
	return absRoot, absTarget, nil
}

func (s WorkspaceService) buildTree(root string, target string, depth int, includeHidden bool, counter *int) (TreeNodeDTO, error) {
	if *counter >= maxTreeEntries {
		return TreeNodeDTO{}, nil
	}
	info, err := os.Stat(target)
	if err != nil {
		return TreeNodeDTO{}, err
	}
	*counter++
	rel, _ := filepath.Rel(root, target)
	if rel == "." {
		rel = ""
	}
	nodeType := "file"
	if info.IsDir() {
		nodeType = "directory"
	}
	node := TreeNodeDTO{
		Path:       filepath.ToSlash(rel),
		Name:       info.Name(),
		Type:       nodeType,
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC(),
	}
	if !info.IsDir() || depth <= 0 {
		return node, nil
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return node, nil
	}
	for _, entry := range entries {
		if *counter >= maxTreeEntries {
			break
		}
		if shouldSkipEntry(entry, includeHidden) {
			continue
		}
		child, err := s.buildTree(root, filepath.Join(target, entry.Name()), depth-1, includeHidden, counter)
		if err != nil {
			continue
		}
		if child.Name != "" {
			node.Children = append(node.Children, child)
		}
	}
	return node, nil
}

func shouldSkipEntry(entry fs.DirEntry, includeHidden bool) bool {
	name := entry.Name()
	if !includeHidden && strings.HasPrefix(name, ".") {
		return true
	}
	if entry.IsDir() {
		switch strings.ToLower(name) {
		case ".git", "node_modules", "dist", "bin", ".task":
			return true
		}
	}
	return false
}

func ParseDepth(value string) int {
	if value == "" {
		return defaultTreeDepth
	}
	depth, err := strconv.Atoi(value)
	if err != nil {
		return defaultTreeDepth
	}
	return depth
}
