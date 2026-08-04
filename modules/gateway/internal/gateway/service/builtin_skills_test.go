package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	protows "redpanda/protocol/ws"
)

func TestEnsureBuiltinSkillsCreatesMissingFiles(t *testing.T) {
	root := t.TempDir()
	if err := EnsureBuiltinSkills(root); err != nil {
		t.Fatal(err)
	}

	for _, skill := range builtinSkills {
		target := filepath.Join(root, skill.name, "SKILL.md")
		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != string(skill.content) || !strings.Contains(string(content), "name: "+skill.name) {
			t.Fatalf("unexpected builtin %s skill content: %q", skill.name, content)
		}
	}
}

func TestEnsureBuiltinSkillsIsIdempotentAndPreservesExisting(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "coding", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("---\nname: coding\ndescription: custom\n---\ncustom instructions\n")
	if err := os.WriteFile(target, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBuiltinSkills(root); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(custom) {
		t.Fatalf("existing skill was overwritten: %q", content)
	}
	if err := EnsureBuiltinSkills(root); err != nil {
		t.Fatal(err)
	}
}

func TestRunAdmissionDoesNotCreateWorkspaceSkills(t *testing.T) {
	root := t.TempDir()
	_, service := newRunServiceTestFixture(t)
	if _, err := service.admitRun(protows.RunStartPayload{
		SessionID: "session_builtin_skill",
		Input:     map[string]any{"text": "inspect the workspace"},
		Options:   map[string]any{"working_dir": root},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, ".codex", "skills")); !os.IsNotExist(err) {
		t.Fatalf("run admission created workspace skill directory: %v", err)
	}
}
