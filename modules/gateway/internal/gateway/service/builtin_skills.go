package service

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed assets/coding/SKILL.md
var builtinCodingSkill []byte

const builtinCodingSkillName = "coding"

// ensureBuiltinCodingSkill installs the Gateway-bundled coding skill only
// when the active workspace does not already provide one. Existing workspace
// content is user-owned and is never overwritten.
func ensureBuiltinCodingSkill(workspaceRoot string) error {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return nil
	}

	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return err
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			// The Gateway does not own workspace creation. A later runtime
			// validation will report the missing workspace with its usual error.
			return nil
		}
		return fmt.Errorf("workspace root is not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace root is not a directory: %q", root)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	realRoot = filepath.Clean(realRoot)

	targetDir := filepath.Join(root, ".codex", "skills", builtinCodingSkillName)
	if err := ensureBuiltinSkillDirectory(realRoot, root, targetDir); err != nil {
		return err
	}
	target := filepath.Join(targetDir, "SKILL.md")
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("builtin skill path is not a regular file: %q", target)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		// Another run may have initialized the same workspace concurrently.
		return ensureBuiltinCodingSkill(workspaceRoot)
	}
	if err != nil {
		return fmt.Errorf("create builtin %s skill: %w", builtinCodingSkillName, err)
	}
	if _, err := file.Write(builtinCodingSkill); err != nil {
		_ = file.Close()
		_ = os.Remove(target)
		return fmt.Errorf("write builtin %s skill: %w", builtinCodingSkillName, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(target)
		return fmt.Errorf("close builtin %s skill: %w", builtinCodingSkillName, err)
	}
	return nil
}

// ensureBuiltinSkillDirectory creates the managed skill directory one
// component at a time and rejects symlinks that resolve outside the workspace.
func ensureBuiltinSkillDirectory(realRoot, lexicalRoot, target string) error {
	rel, err := filepath.Rel(lexicalRoot, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("builtin skill path escapes workspace root")
	}
	current := lexicalRoot
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0o755); err != nil {
				if !os.IsExist(err) {
					return fmt.Errorf("create builtin skill directory: %w", err)
				}
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			evaluated, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return fmt.Errorf("resolve builtin skill directory: %w", evalErr)
			}
			if !pathInside(realRoot, evaluated) {
				return fmt.Errorf("builtin skill path escapes workspace root")
			}
			info, statErr = os.Stat(evaluated)
			if statErr != nil {
				return statErr
			}
		}
		if !info.IsDir() {
			return fmt.Errorf("builtin skill path is not a directory: %q", current)
		}
	}
	return nil
}

func pathInside(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
