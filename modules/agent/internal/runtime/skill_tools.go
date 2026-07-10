package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const managedSkillsPath = ".codex/skills"
const maxSkillDescriptionBytes = 4 * 1024
const maxSkillInstructionsBytes = 256 * 1024

var managedSkillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func runCreateSkill(workspaceRoot string, name string, description string, instructions string) (string, error) {
	content, err := renderManagedSkill(name, description, instructions)
	if err != nil {
		return "", err
	}
	target, err := managedSkillFile(workspaceRoot, name, true)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return "", fmt.Errorf("skill %q already exists", name)
	}
	if err != nil {
		return "", err
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(target)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(target)
		return "", err
	}
	return managedSkillOutput("skill.create", name), nil
}

func runUpdateSkill(workspaceRoot string, name string, description string, instructions string) (string, error) {
	content, err := renderManagedSkill(name, description, instructions)
	if err != nil {
		return "", err
	}
	target, err := managedSkillFile(workspaceRoot, name, false)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("skill %q not found", name)
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("skill %q SKILL.md is not a regular file", name)
	}

	temp, err := os.CreateTemp(filepath.Dir(target), ".SKILL.md-*")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		_ = temp.Close()
		return "", err
	}
	if _, err := temp.WriteString(content); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, target); err != nil {
		return "", err
	}
	return managedSkillOutput("skill.update", name), nil
}

func renderManagedSkill(name string, description string, instructions string) (string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	instructions = strings.TrimSpace(instructions)
	if !managedSkillNamePattern.MatchString(name) {
		return "", fmt.Errorf("skill name must be lowercase ASCII and use only letters, digits, and hyphens")
	}
	if description == "" {
		return "", fmt.Errorf("skill description is required")
	}
	if len(description) > maxSkillDescriptionBytes {
		return "", fmt.Errorf("skill description exceeds %d bytes", maxSkillDescriptionBytes)
	}
	if instructions == "" {
		return "", fmt.Errorf("skill instructions are required")
	}
	if len(instructions) > maxSkillInstructionsBytes {
		return "", fmt.Errorf("skill instructions exceed %d bytes", maxSkillInstructionsBytes)
	}
	descriptionJSON, err := json.Marshal(description)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, descriptionJSON, instructions), nil
}

func managedSkillFile(workspaceRoot string, name string, create bool) (string, error) {
	if !managedSkillNamePattern.MatchString(strings.TrimSpace(name)) {
		return "", fmt.Errorf("skill name must be lowercase ASCII and use only letters, digits, and hyphens")
	}
	root, err := cleanWorkspaceRoot(workspaceRoot)
	if err != nil {
		return "", err
	}
	codexDirectory, err := managedSkillDirectory(root, filepath.Join(root, ".codex"), create, name)
	if err != nil {
		return "", err
	}
	base, err := managedSkillDirectory(codexDirectory, filepath.Join(codexDirectory, "skills"), create, name)
	if err != nil {
		return "", err
	}
	directory, err := managedSkillDirectory(base, filepath.Join(base, name), create, name)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "SKILL.md"), nil
}

func managedSkillDirectory(parent string, path string, create bool, skillName string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if !create {
			return "", fmt.Errorf("skill %q not found", skillName)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			return "", err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("managed skill path is not a directory")
	}
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	evaluated = filepath.Clean(evaluated)
	if !isPathInside(parent, evaluated) {
		return "", fmt.Errorf("skill path escapes managed skill root")
	}
	evaluatedInfo, err := os.Stat(evaluated)
	if err != nil {
		return "", err
	}
	if !evaluatedInfo.IsDir() {
		return "", fmt.Errorf("managed skill path is not a directory")
	}
	return evaluated, nil
}

func managedSkillOutput(action string, name string) string {
	raw, _ := json.Marshal(map[string]string{
		"action": action,
		"name":   name,
		"path":   filepath.ToSlash(filepath.Join(managedSkillsPath, name, "SKILL.md")),
	})
	return string(raw)
}
