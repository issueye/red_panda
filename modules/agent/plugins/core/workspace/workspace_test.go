package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
)

func TestRegisterReturnsAllTools(t *testing.T) {
	reg := registry.NewRegistry()
	bus := hooks.NewBus()
	names := Register(reg, bus)

	if len(names) != len(tools) {
		t.Fatalf("Register returned %d names, expected %d", len(names), len(tools))
	}
	for i, name := range names {
		if name != tools[i].Definition.Name {
			t.Errorf("tool[%d] name = %q, want %q", i, name, tools[i].Definition.Name)
		}
	}
}

func TestAllToolDefinitionsMatchOriginalSchema(t *testing.T) {
	expected := map[string]struct {
		DisplayName string
		Risk        string
	}{
		"workspace.read_file":   {DisplayName: "Read file", Risk: "low"},
		"workspace.list":        {DisplayName: "List files", Risk: "low"},
		"workspace.stats":       {DisplayName: "Workspace stats", Risk: "low"},
		"workspace.grep":        {DisplayName: "Search files", Risk: "low"},
		"workspace.find_files":  {DisplayName: "Find files", Risk: "low"},
		"workspace.read_files":  {DisplayName: "Read files", Risk: "low"},
		"workspace.write_file":  {DisplayName: "Write file", Risk: "high"},
		"workspace.edit_file":   {DisplayName: "Edit file", Risk: "high"},
		"workspace.diff_file":   {DisplayName: "Preview diff", Risk: "low"},
		"workspace.apply_patch": {DisplayName: "Apply patch", Risk: "high"},
	}
	for _, entry := range tools {
		exp, ok := expected[entry.Definition.Name]
		if !ok {
			t.Errorf("unexpected tool %q", entry.Definition.Name)
			continue
		}
		if entry.TimeoutClass != registry.LocalToolTimeout {
			t.Errorf("%s: TimeoutClass = %v, want LocalToolTimeout", entry.Definition.Name, entry.TimeoutClass)
		}
		if entry.Source != "builtin:workspace" {
			t.Errorf("%s: Source = %q, want %q", entry.Definition.Name, entry.Source, "builtin:workspace")
		}
		if entry.Definition.DisplayName != exp.DisplayName {
			t.Errorf("%s: DisplayName = %q, want %q", entry.Definition.Name, entry.Definition.DisplayName, exp.DisplayName)
		}
		if string(entry.Definition.Risk) != exp.Risk {
			t.Errorf("%s: Risk = %q, want %q", entry.Definition.Name, entry.Definition.Risk, exp.Risk)
		}
	}
}

func TestReadFileHandlerSuccessAndNotFound(t *testing.T) {
	dir := t.TempDir()
	file := "hello.txt"
	content := "hello world"
	if err := os.WriteFile(fmt.Sprintf("%s/%s", dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	toolCtx := &registry.ToolContext{WorkingDir: dir}

	res, err := runReadFileHandler(ctx, toolCtx, map[string]any{"path": file})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.Status != "completed" {
		t.Errorf("expected status completed, got %s", res.Status)
	}
	if res.Output != content {
		t.Errorf("expected output %q, got %q", content, res.Output)
	}

	// Not-found case
	res, err = runReadFileHandler(ctx, toolCtx, map[string]any{"path": "nope.txt"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if res.Status != "failed" {
		t.Errorf("expected status failed, got %s", res.Status)
	}
}

func TestWriteFileHandler(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	toolCtx := &registry.ToolContext{WorkingDir: dir}

	content := "new content"
	res, err := runWriteFileHandler(ctx, toolCtx, map[string]any{"path": "new.txt", "content": content})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.Status != "completed" {
		t.Errorf("expected status completed, got %s", res.Status)
	}

	got, err := os.ReadFile(fmt.Sprintf("%s/new.txt", dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("expected %q, got %q", content, string(got))
	}
}

func TestListHandler(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(fmt.Sprintf("%s/a.txt", dir), []byte("a"), 0o644)
	os.WriteFile(fmt.Sprintf("%s/b.txt", dir), []byte("b"), 0o644)
	os.MkdirAll(fmt.Sprintf("%s/sub", dir), 0o755)

	ctx := context.Background()
	toolCtx := &registry.ToolContext{WorkingDir: dir}
	res, err := runListHandler(ctx, toolCtx, map[string]any{"path": ".", "max_depth": 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected completed, got %s", res.Status)
	}
	output := res.Output
	if output != "./\na.txt\nb.txt\nsub/" {
		t.Errorf("unexpected output: %q", output)
	}
}

func TestGrepHandler(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(fmt.Sprintf("%s/test.txt", dir), []byte("line one\nfoo bar\nline three\n"), 0o644)

	ctx := context.Background()
	toolCtx := &registry.ToolContext{WorkingDir: dir}
	res, err := runGrepHandler(ctx, toolCtx, map[string]any{"pattern": "foo", "path": "."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected completed, got %s", res.Status)
	}
	if res.Output != "test.txt:2: foo bar" {
		t.Errorf("unexpected output: %q", res.Output)
	}
}

func TestStatsHandler(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(fmt.Sprintf("%s/a.txt", dir), []byte("a"), 0o644)
	os.WriteFile(fmt.Sprintf("%s/b.txt", dir), []byte("b"), 0o644)

	ctx := context.Background()
	toolCtx := &registry.ToolContext{WorkingDir: dir}
	res, err := runStatsHandler(ctx, toolCtx, map[string]any{"path": "."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected completed, got %s", res.Status)
	}
	// output should contain total_files: 2
	if len(res.Output) == 0 {
		t.Error("expected non-empty output from stats")
	}
}

func TestFindFilesHandler(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(fmt.Sprintf("%s/foo.txt", dir), []byte("a"), 0o644)
	os.WriteFile(fmt.Sprintf("%s/bar.go", dir), []byte("b"), 0o644)

	ctx := context.Background()
	toolCtx := &registry.ToolContext{WorkingDir: dir}
	res, err := runFindFilesHandler(ctx, toolCtx, map[string]any{"pattern": "foo", "path": "."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected completed, got %s", res.Status)
	}
	if res.Output != "foo.txt" {
		t.Errorf("unexpected output: %q", res.Output)
	}
}

func TestEditDiffAndPatchHandlers(t *testing.T) {
	dir := t.TempDir()
	target := fmt.Sprintf("%s/note.txt", dir)
	if err := os.WriteFile(target, []byte("alpha red panda\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	toolCtx := &registry.ToolContext{WorkingDir: dir}

	diff, err := runDiffFileHandler(context.Background(), toolCtx, map[string]any{
		"path": "note.txt", "old_text": "red panda", "new_text": "scarlet panda",
	})
	if err != nil || !strings.Contains(diff.Output, "-alpha red panda") || !strings.Contains(diff.Output, "+alpha scarlet panda") {
		t.Fatalf("diff result = %#v, %v", diff, err)
	}
	if raw, _ := os.ReadFile(target); string(raw) != "alpha red panda\n" {
		t.Fatalf("diff wrote file: %q", raw)
	}

	edited, err := runEditFileHandler(context.Background(), toolCtx, map[string]any{
		"path": "note.txt", "old_text": "red panda", "new_text": "scarlet panda",
	})
	if err != nil || edited.Status != "completed" {
		t.Fatalf("edit result = %#v, %v", edited, err)
	}

	patch := "--- a/note.txt\n+++ b/note.txt\n@@ -1 +1 @@\n-alpha scarlet panda\n+beta\n"
	patched, err := runApplyPatchHandler(context.Background(), toolCtx, map[string]any{"patch": patch})
	if err != nil || patched.Status != "completed" {
		t.Fatalf("patch result = %#v, %v", patched, err)
	}
	if raw, _ := os.ReadFile(target); string(raw) != "beta\n" {
		t.Fatalf("patched content = %q", raw)
	}
}

func TestMutationHandlersRejectEscapingPaths(t *testing.T) {
	toolCtx := &registry.ToolContext{WorkingDir: t.TempDir()}
	if _, err := runEditFileHandler(context.Background(), toolCtx, map[string]any{
		"path": "../outside.txt", "old_text": "a", "new_text": "b",
	}); err == nil {
		t.Fatal("edit accepted escaping path")
	}
	patch := "--- a/../outside.txt\n+++ b/../outside.txt\n@@ -1 +1 @@\n-a\n+b\n"
	if _, err := runApplyPatchHandler(context.Background(), toolCtx, map[string]any{"patch": patch}); err == nil {
		t.Fatal("patch accepted escaping path")
	}
}

func TestReadFilesAndGlobHandlers(t *testing.T) {
	dir := t.TempDir()
	for path, content := range map[string]string{
		"a.txt": "alpha", "pkg/main_test.go": "package pkg", "root_test.go": "package root",
		"node_modules/ignored_test.go": "ignored",
	} {
		full := fmt.Sprintf("%s/%s", dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	toolCtx := &registry.ToolContext{WorkingDir: dir}
	read, err := runReadFilesHandler(context.Background(), toolCtx, map[string]any{"paths": []any{"a.txt", "root_test.go"}})
	if err != nil || !strings.Contains(read.Output, "--- a.txt ---") || !strings.Contains(read.Output, "alpha") || !strings.Contains(read.Output, "--- root_test.go ---") {
		t.Fatalf("read_files result = %#v, %v", read, err)
	}
	found, err := runFindFilesHandler(context.Background(), toolCtx, map[string]any{"pattern": "**/*_test.go", "path": "."})
	if err != nil || !strings.Contains(found.Output, "pkg/main_test.go") || !strings.Contains(found.Output, "root_test.go") || strings.Contains(found.Output, "ignored_test.go") {
		t.Fatalf("find_files result = %#v, %v", found, err)
	}
}
