package tools

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFindFilesSupportsGlobAndSkipsGeneratedDirectories(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pkg", "main.go"), "package pkg\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "main_test.go"), "package pkg\n")
	mustWriteFile(t, filepath.Join(root, "root_test.go"), "package root\n")
	mustWriteFile(t, filepath.Join(root, "node_modules", "ignored.go"), "package ignored\n")

	output, err := runFindFiles(root, ".", "**/*_test.go", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "pkg/main_test.go") || !strings.Contains(output, "root_test.go") || strings.Contains(output, "ignored.go") {
		t.Fatalf("unexpected find output: %s", output)
	}
}

func TestRunReadFilesAddsFileHeadings(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "alpha")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "beta")

	output, err := runReadFiles(root, []string{"a.txt", "b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"--- a.txt ---", "alpha", "--- b.txt ---", "beta"} {
		if !strings.Contains(output, text) {
			t.Fatalf("batch read missing %q: %s", text, output)
		}
	}
}

func TestRunGitStatusAndDiffAreReadOnly(t *testing.T) {
	root := t.TempDir()
	runGitTestCommand(t, root, "init")
	mustWriteFile(t, filepath.Join(root, "note.txt"), "alpha\n")

	status, err := runGitStatus(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "note.txt") {
		t.Fatalf("status missing untracked file: %s", status)
	}

	runGitTestCommand(t, root, "add", "note.txt")
	diff, err := runGitDiff(context.Background(), root, true, "", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+alpha") {
		t.Fatalf("staged diff missing content: %s", diff)
	}
}

func runGitTestCommand(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
