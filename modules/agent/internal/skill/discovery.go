package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	pathutil "redpanda/agent/internal/pathutil"
	"sort"
	"strings"

	"redpanda/protocol/methods"
)

// buildSkillsContext 从磁盘加载单个会话所需的最新受管技能。
// 每次都会重新读取，使新建技能能够立即使用。
func BuildContext(workspaceRoot string) *methods.SkillsContext {
	root := strings.TrimSpace(workspaceRoot)
	items, err := ListAvailable(root)
	if err != nil {
		return &methods.SkillsContext{
			Context: "Managed skills: failed to load skill directories (" + err.Error() + "). Use skill.list to retry.",
		}
	}
	return &methods.SkillsContext{
		Items:   items,
		Context: formatSkillsCatalog(items),
	}
}

func formatSkillsCatalog(items []methods.SkillSummary) string {
	var b strings.Builder
	b.WriteString("Managed skills catalog (refreshed for this conversation from the Gateway skill directory and workspace .codex/skills).\n")
	b.WriteString("When a skill matches the user task, read its SKILL.md with workspace.read_file using the listed path and decide how to apply it. Do not use a separate skill runner.\n")
	b.WriteString("Use skill.list to re-check, skill.create/update/delete to manage skills.\n")
	if len(items) == 0 {
		b.WriteString("Currently available skills: none.\n")
		return strings.TrimSpace(b.String())
	}
	b.WriteString("Currently available skills:\n")
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		desc := strings.TrimSpace(item.Description)
		if desc == "" {
			desc = "(no description)"
		}
		b.WriteString("- ")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(desc)
		if path := strings.TrimSpace(item.Path); path != "" {
			b.WriteString(" (path: ")
			b.WriteString(path)
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// ListAvailable returns Gateway-owned built-in skills plus workspace-managed
// skills. Built-ins win on duplicate names so every workspace uses the same
// bundled instructions without copying them into the workspace.
func ListAvailable(workspaceRoot string) ([]methods.SkillSummary, error) {
	items := make([]methods.SkillSummary, 0)
	globalRoot, err := builtinSkillsRoot()
	if err != nil {
		return nil, err
	}
	if globalRoot != "" {
		globalItems, err := listSkillRoot(globalRoot, true)
		if err != nil {
			return nil, err
		}
		items = append(items, globalItems...)
	}
	if strings.TrimSpace(workspaceRoot) != "" {
		workspaceItems, err := ListManaged(workspaceRoot)
		if err != nil {
			return nil, err
		}
		seen := make(map[string]struct{}, len(items))
		for _, item := range items {
			seen[item.Name] = struct{}{}
		}
		for _, item := range workspaceItems {
			if _, exists := seen[item.Name]; exists {
				continue
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func builtinSkillsRoot() (string, error) {
	configured := strings.TrimSpace(os.Getenv(methods.EnvSkillsDir))
	if configured == "" {
		return "", nil
	}
	root, err := filepath.Abs(configured)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("Gateway skill directory is not accessible: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Gateway skill directory is not a directory: %q", root)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func ListManaged(workspaceRoot string) ([]methods.SkillSummary, error) {
	root, err := pathutil.CleanWorkspaceRoot(workspaceRoot)
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
		// 技能目录和会话注入仅需描述信息。
		detail, err := LoadManagedDetail(root, name, false)
		if err != nil {
			continue
		}
		items = append(items, methods.SkillSummary{
			Name:            detail.Name,
			Description:     detail.Description,
			Path:            detail.Path,
			HasInstructions: true, // managed skills always have a SKILL.md body when listable
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func listSkillRoot(root string, absolutePaths bool) ([]methods.SkillSummary, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	items := make([]methods.SkillSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !managedSkillNamePattern.MatchString(entry.Name()) {
			continue
		}
		name := entry.Name()
		target := filepath.Join(root, name, "SKILL.md")
		displayPath := filepath.ToSlash(filepath.Join(managedSkillsPath, name, "SKILL.md"))
		if absolutePaths {
			displayPath = filepath.ToSlash(target)
		}
		detail, err := loadSkillDetail(target, name, false, displayPath)
		if err != nil {
			continue
		}
		items = append(items, methods.SkillSummary{
			Name:            detail.Name,
			Description:     detail.Description,
			Path:            detail.Path,
			HasInstructions: true,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func LoadManagedDetail(workspaceRoot string, name string, includeInstructions bool) (methods.SkillDetail, error) {
	name = strings.TrimSpace(name)
	if !managedSkillNamePattern.MatchString(name) {
		return methods.SkillDetail{}, fmt.Errorf("skill name must be lowercase ASCII and use only letters, digits, and hyphens")
	}
	path, err := ManagedFile(workspaceRoot, name, false)
	if err != nil {
		return methods.SkillDetail{}, err
	}
	return loadSkillDetail(path, name, includeInstructions, filepath.ToSlash(filepath.Join(managedSkillsPath, name, "SKILL.md")))
}

func loadSkillDetail(path string, name string, includeInstructions bool, displayPath string) (methods.SkillDetail, error) {
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
	if info.Size() > MaxDescriptionBytes+MaxInstructionsBytes+1024 {
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
		// 优先使用目录名作为规范标识，并保留解析出的描述和指令。
	}
	detail := methods.SkillDetail{
		Name:        name,
		Description: parsed.Description,
		Path:        displayPath,
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
		// 允许以 --- 结尾且正文末尾没有换行。
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
	// 接受手工编辑文件中的无引号纯文本描述。
	return strings.Trim(raw, `"'`)
}

func RunList(workspaceRoot string) (string, error) {
	items, err := ListAvailable(workspaceRoot)
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

func RunDelete(workspaceRoot string, name string) (string, error) {
	name = strings.TrimSpace(name)
	if !managedSkillNamePattern.MatchString(name) {
		return "", fmt.Errorf("skill name must be lowercase ASCII and use only letters, digits, and hyphens")
	}
	// managedSkillFile 校验目录边界和符号链接越界。
	target, err := ManagedFile(workspaceRoot, name, false)
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
	// 尽力删除空技能目录。
	_ = os.Remove(skillDir)

	raw, _ := json.Marshal(map[string]any{
		"action":  "skill.delete",
		"name":    name,
		"deleted": true,
		"path":    filepath.ToSlash(filepath.Join(managedSkillsPath, name, "SKILL.md")),
	})
	return string(raw), nil
}
