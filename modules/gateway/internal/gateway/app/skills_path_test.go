package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGatewaySkillsDirectoryDefaultsBesideExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got, err := gatewaySkillsDirectory("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(filepath.Dir(exe), "skills"))
	if got != want {
		t.Fatalf("Gateway skills directory = %q, want %q", got, want)
	}
}

func TestGatewaySkillsDirectoryUsesConfiguredAbsolutePath(t *testing.T) {
	want := filepath.Join(t.TempDir(), "shared-skills")
	got, err := gatewaySkillsDirectory(want)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("Gateway skills directory = %q, want %q", got, want)
	}
}
