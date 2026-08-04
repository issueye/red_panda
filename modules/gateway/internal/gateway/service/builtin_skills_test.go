package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	protows "redpanda/protocol/ws"
)

func TestEnsureBuiltinCodingSkillCreatesMissingFile(t *testing.T) {
	root := t.TempDir()
	if err := ensureBuiltinCodingSkill(root); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, ".codex", "skills", "coding", "SKILL.md")
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(builtinCodingSkill) || !strings.Contains(string(content), "name: coding") {
		t.Fatalf("unexpected builtin skill content: %q", content)
	}
}

func TestEnsureBuiltinCodingSkillIsIdempotentAndPreservesExisting(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, ".codex", "skills", "coding", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("---\nname: coding\ndescription: custom\n---\ncustom instructions\n")
	if err := os.WriteFile(target, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureBuiltinCodingSkill(root); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(custom) {
		t.Fatalf("existing skill was overwritten: %q", content)
	}
	if err := ensureBuiltinCodingSkill(root); err != nil {
		t.Fatal(err)
	}
}

func TestRunAdmissionEnsuresBuiltinCodingSkill(t *testing.T) {
	root := t.TempDir()
	_, service := newRunServiceTestFixture(t)
	if _, err := service.admitRun(protows.RunStartPayload{
		SessionID: "session_builtin_skill",
		Input:     map[string]any{"text": "inspect the workspace"},
		Options:   map[string]any{"working_dir": root},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, ".codex", "skills", "coding", "SKILL.md")); err != nil {
		t.Fatalf("run admission did not install builtin coding skill: %v", err)
	}
}
