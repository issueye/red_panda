package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"redpanda/protocol/methods"
)

func listManagedSkills(workspaceRoot string) ([]methods.SkillSummary, error) {
	root, err := cleanWorkspaceRoot(workspaceRoot)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, managedSkillsPath)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return []methods.SkillSummary{}, nil
		}
		return nil, err
	}

	items := make([]methods.SkillSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !managedSkillNamePattern.MatchString(name) {
			continue
		}
		detail, err := loadManagedSkillDetail(root, name, true)
		if err != nil {
			continue
		}
		items = append(items, methods.SkillSummary{
			Name:            detail.Name,
			Description:     detail.Description,
			Path:            detail.Path,
			HasInstructions: strings.TrimSpace(detail.Instructions) != "",
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func loadManagedSkillDetail(workspaceRoot string, name string, includeInstructions bool) (methods.SkillDetail, error) {
	name = strings.TrimSpace(name)
	if !managedSkillNamePattern.MatchString(name) {
		return methods.SkillDetail{}, fmt.Errorf("skill name must be lowercase ASCII and use only letters, digits, and hyphens")
	}
	path, err := managedSkillFile(workspaceRoot, name, false)
	if err != nil {
		return methods.SkillDetail{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return methods.SkillDetail{}, fmt.Errorf("skill %q not found", name)
		}
		return methods.SkillDetail{}, err
	}
	if !info.Mode().IsRegular() {
		return methods.SkillDetail{}, fmt.Errorf("skill %q SKILL.md is not a regular file", name)
	}
	if info.Size() > maxSkillDescriptionBytes+maxSkillInstructionsBytes+1024 {
		return methods.SkillDetail{}, fmt.Errorf("skill %q exceeds the managed size limit", name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return methods.SkillDetail{}, err
	}
	parsed, err := parseManagedSkillMarkdown(string(raw))
	if err != nil {
		return methods.SkillDetail{}, err
	}
	if parsed.Name != "" && parsed.Name != name {
		// Prefer directory name as canonical identity; keep parsed description/instructions.
	}
	detail := methods.SkillDetail{
		Name:        name,
		Description: parsed.Description,
		Path:        filepath.ToSlash(filepath.Join(managedSkillsPath, name, "SKILL.md")),
		SizeBytes:   info.Size(),
	}
	if includeInstructions {
		detail.Instructions = parsed.Instructions
	}
	return detail, nil
}

type parsedManagedSkill struct {
	Name         string
	Description  string
	Instructions string
}

func parseManagedSkillMarkdown(content string) (parsedManagedSkill, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(content, "---\n") {
		return parsedManagedSkill{}, fmt.Errorf("skill missing frontmatter")
	}
	rest := strings.TrimPrefix(content, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		// allow trailing --- without trailing newline body
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
			front := rest[:end]
			return parsedManagedSkill{
				Name:        frontmatterValue(front, "name"),
				Description: decodeFrontmatterDescription(frontmatterValue(front, "description")),
			}, nil
		}
		return parsedManagedSkill{}, fmt.Errorf("skill frontmatter is not closed")
	}
	front := rest[:end]
	body := strings.TrimSpace(rest[end+len("\n---\n"):])
	name := frontmatterValue(front, "name")
	description := decodeFrontmatterDescription(frontmatterValue(front, "description"))
	if description == "" {
		return parsedManagedSkill{}, fmt.Errorf("skill description is required")
	}
	return parsedManagedSkill{
		Name:         name,
		Description:  description,
		Instructions: body,
	}, nil
}

func frontmatterValue(front string, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	return ""
}

func decodeFrontmatterDescription(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded string
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		return strings.TrimSpace(decoded)
	}
	// Accept plain unquoted description for hand-edited files.
	return strings.Trim(raw, `"'`)
}

func runListSkills(workspaceRoot string) (string, error) {
	items, err := listManagedSkills(workspaceRoot)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(map[string]any{
		"action": "skill.list",
		"count":  len(items),
		"items":  items,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func runDeleteSkill(workspaceRoot string, name string) (string, error) {
	name = strings.TrimSpace(name)
	if !managedSkillNamePattern.MatchString(name) {
		return "", fmt.Errorf("skill name must be lowercase ASCII and use only letters, digits, and hyphens")
	}
	// managedSkillFile validates directory boundaries and symlink escape.
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
	skillDir := filepath.Dir(target)
	if err := os.Remove(target); err != nil {
		return "", err
	}
	// Best-effort remove empty skill directory.
	_ = os.Remove(skillDir)

	raw, _ := json.Marshal(map[string]any{
		"action":  "skill.delete",
		"name":    name,
		"deleted": true,
		"path":    filepath.ToSlash(filepath.Join(managedSkillsPath, name, "SKILL.md")),
	})
	return string(raw), nil
}
