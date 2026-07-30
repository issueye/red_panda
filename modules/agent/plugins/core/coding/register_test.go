package coding

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	"redpanda/agent/plugins/core/coding/internal"
)

func TestRegisterReturnsFiveTools(t *testing.T) {
	reg := registry.NewRegistry()
	bus := hooks.NewBus()
	names := Register(reg, bus)
	if len(names) != 5 {
		t.Fatalf("expected 5 tools, got %d: %v", len(names), names)
	}
	expected := []string{"git.status", "git.diff", "git.log", "git.show", "shell.exec"}
	for i, name := range expected {
		if names[i] != name {
			t.Fatalf("names[%d] = %q, want %q", i, names[i], name)
		}
	}
}

func TestRegisterEntriesHaveCorrectSource(t *testing.T) {
	reg := registry.NewRegistry()
	bus := hooks.NewBus()
	Register(reg, bus)
	for _, name := range []string{"git.status", "git.diff", "git.log", "git.show", "shell.exec"} {
		e, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("tool %q not found", name)
		}
		if e.Source != "plugin:coding" {
			t.Fatalf("%s source = %q, want %q", name, e.Source, "plugin:coding")
		}
		if e.OpsOnly {
			t.Fatalf("%s opsOnly = true, want false", name)
		}
	}
}

func TestRegisterTimeoutClasses(t *testing.T) {
	reg := registry.NewRegistry()
	bus := hooks.NewBus()
	Register(reg, bus)
	for _, name := range []string{"git.status", "git.diff", "git.log", "git.show"} {
		e, _ := reg.Lookup(name)
		if e.TimeoutClass != registry.LocalToolTimeout {
			t.Fatalf("%s timeout = %v, want %v", name, e.TimeoutClass, registry.LocalToolTimeout)
		}
	}
	e, _ := reg.Lookup("shell.exec")
	if e.TimeoutClass != registry.SelfManagedToolTimeout {
		t.Fatalf("shell.exec timeout = %v, want %v", e.TimeoutClass, registry.SelfManagedToolTimeout)
	}
}

func TestGitStatus(t *testing.T) {
	root := t.TempDir()
	mustGit(t, root, "init")
	mustWriteFile(t, filepath.Join(root, "note.txt"), "alpha\n")

	output, err := internal.RunGitStatus(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "note.txt") {
		t.Fatalf("status missing untracked file: %s", output)
	}
}

func TestGitDiffStaged(t *testing.T) {
	root := t.TempDir()
	mustGit(t, root, "init")
	mustWriteFile(t, filepath.Join(root, "note.txt"), "alpha\n")
	mustGit(t, root, "add", "note.txt")

	output, err := internal.RunGitDiff(context.Background(), root, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "+alpha") {
		t.Fatalf("staged diff missing content: %s", output)
	}
}

func TestGitLog(t *testing.T) {
	root := t.TempDir()
	mustGit(t, root, "init")
	mustGit(t, root, "commit", "--allow-empty", "-m", "initial")

	output, err := internal.RunGitLog(context.Background(), root, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "initial") {
		t.Fatalf("log missing commit: %s", output)
	}
}

func TestGitShow(t *testing.T) {
	root := t.TempDir()
	mustGit(t, root, "init")
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello\n")
	mustGit(t, root, "add", "a.txt")
	mustGit(t, root, "commit", "-m", "add a.txt")

	output, err := internal.RunGitShow(context.Background(), root, "HEAD", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "add a.txt") {
		t.Fatalf("show missing commit message: %s", output)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
