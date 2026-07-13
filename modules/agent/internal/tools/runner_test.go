package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

func TestRunWorkspaceStatsSuggestsSplitsForLargeTree(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 60; i++ {
		mustWriteFile(t, filepath.Join(root, "modules", "a", fmt.Sprintf("f%d.go", i)), "package a\n")
		mustWriteFile(t, filepath.Join(root, "modules", "b", fmt.Sprintf("f%d.go", i)), "package b\n")
	}
	raw, err := runWorkspaceStats(root, ".", 4)
	if err != nil {
		t.Fatal(err)
	}
	var stats workspaceStatsResult
	if err := json.Unmarshal([]byte(raw), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.TotalFiles < 100 {
		t.Fatalf("total_files = %d, want >= 100", stats.TotalFiles)
	}
	if stats.SuggestedSplits < 2 {
		t.Fatalf("suggested_splits = %d, want >= 2", stats.SuggestedSplits)
	}
	if stats.SuggestedMaxTurns != stats.TotalFiles+8 {
		t.Fatalf("suggested_max_turns = %d, want files+summary %d", stats.SuggestedMaxTurns, stats.TotalFiles+8)
	}
	if len(stats.TopLevel) == 0 || stats.TopLevel[0].RecommendedMaxTurns != stats.TopLevel[0].Files+8 {
		t.Fatalf("top_level recommended_max_turns missing/wrong: %#v", stats.TopLevel)
	}
	if !strings.Contains(stats.SplitGuidance, "split") && !strings.Contains(stats.SplitGuidance, "multiple") {
		t.Fatalf("split_guidance = %q", stats.SplitGuidance)
	}
}

func TestRunListWorkspace(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "README.md"), "red panda\n")
	mustWriteFile(t, filepath.Join(root, "cmd", "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "node_modules", "ignored.js"), "ignored\n")

	output, err := runListWorkspace(root, ".", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"./", "README.md", "cmd/", "cmd/main.go"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected list output to contain %q, got:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "node_modules/ignored.js") {
		t.Fatalf("expected skipped directory to be omitted, got:\n%s", output)
	}
}

func TestRunGrepWorkspace(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "README.md"), "red panda\nblack bear\n")
	mustWriteFile(t, filepath.Join(root, "docs", "notes.md"), "red fox\nred panda again\n")
	mustWriteFile(t, filepath.Join(root, "bin", "ignored.txt"), "red panda hidden\n")

	output, err := runGrepWorkspace(root, "red panda", ".", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"README.md:1: red panda", "docs/notes.md:2: red panda again"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected grep output to contain %q, got:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "bin/ignored.txt") {
		t.Fatalf("expected skipped directory to be omitted, got:\n%s", output)
	}
}

func TestWorkspaceToolsRejectTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := runListWorkspace(root, "..", 1)
	if err == nil || !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
	_, err = runGrepWorkspace(root, "anything", "..", 10)
	if err == nil || !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestRunReadFileMissingPathIsActionable(t *testing.T) {
	root := t.TempDir()
	_, err := runReadFile(root, "OfficeCli/README_zh.md")
	if err == nil {
		t.Fatal("expected missing file error")
	}
	msg := err.Error()
	for _, want := range []string{"file not found", "OfficeCli/README_zh.md", "working_dir"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q should contain %q", msg, want)
		}
	}
}

func TestResolveWorkspacePathRejectsMissingWorkingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := resolveWorkspacePath(missing, "README.md")
	if err == nil || !strings.Contains(err.Error(), "working_dir does not exist") {
		t.Fatalf("expected missing working_dir error, got %v", err)
	}
}

func TestRunBoundedTimesOutStuckTool(t *testing.T) {
	started := time.Now()
	_, err := runBounded(context.Background(), 50*time.Millisecond, func(context.Context) (string, error) {
		// 模拟永不响应上下文取消的卡住文件系统或 RPC 调用。
		time.Sleep(2 * time.Second)
		return "too slow", nil
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
}

func TestRunBoundedNoTimeoutWhenDisabled(t *testing.T) {
	out, err := runBounded(context.Background(), 0, func(context.Context) (string, error) {
		return "ok", nil
	})
	if err != nil || out != "ok" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestToolTimeoutForSelfManagedTools(t *testing.T) {
	if got := toolTimeoutFor("shell.exec"); got != 0 {
		t.Fatalf("shell.exec timeout = %s, want 0 (self-managed)", got)
	}
	if got := toolTimeoutFor("workspace.read_file"); got != defaultLocalToolTimeout {
		t.Fatalf("workspace.read_file timeout = %s, want %s", got, defaultLocalToolTimeout)
	}
}

func TestRunEditFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	mustWriteFile(t, target, "hello red panda\n")

	output, err := runEditFile(root, "note.txt", "red panda", "scarlet panda", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "replaced 1 occurrence") {
		t.Fatalf("unexpected edit output: %s", output)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello scarlet panda\n" {
		t.Fatalf("edited content = %q", string(raw))
	}
}

func TestRunEditFileRequiresUniqueMatchByDefault(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "note.txt"), "red panda\nred panda\n")

	_, err := runEditFile(root, "note.txt", "red panda", "scarlet panda", false)
	if err == nil || !strings.Contains(err.Error(), "occurs 2 times") {
		t.Fatalf("expected duplicate match error, got %v", err)
	}
	output, err := runEditFile(root, "note.txt", "red panda", "scarlet panda", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "replaced 2 occurrence") {
		t.Fatalf("unexpected edit output: %s", output)
	}
}

func TestRunDiffFilePreviewsReplacementWithoutWriting(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	mustWriteFile(t, target, "alpha\n")

	output, err := runDiffFile(root, "note.txt", "", false, "alpha", "beta", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--- a/note.txt", "+++ b/note.txt", "-alpha", "+beta"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected diff output to contain %q, got:\n%s", expected, output)
		}
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "alpha\n" {
		t.Fatalf("diff preview should not write file, got %q", string(raw))
	}
}

func TestRunApplyPatchAppliesUnifiedPatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	mustWriteFile(t, target, "alpha\n")
	patch := `diff --git a/note.txt b/note.txt
index 4a58007..65b2df8 100644
--- a/note.txt
+++ b/note.txt
@@ -1 +1 @@
-alpha
+beta
`

	output, err := runApplyPatch(root, patch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "applied patch to 1 file") {
		t.Fatalf("unexpected patch output: %s", output)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "beta\n" {
		t.Fatalf("patched content = %q", string(raw))
	}
}

func TestRunApplyPatchRejectsEscapingPath(t *testing.T) {
	root := t.TempDir()
	patch := `--- a/../outside.txt
+++ b/../outside.txt
@@ -1 +1 @@
-alpha
+beta
`
	_, err := runApplyPatch(root, patch)
	if err == nil || !strings.Contains(err.Error(), "invalid patch path") {
		t.Fatalf("expected invalid patch path error, got %v", err)
	}
}

func TestToolRunnerAcceptsListAndGrepCalls(t *testing.T) {
	runner := ToolRunner{}
	for _, call := range []ptools.Call{
		{Name: "workspace.list", Arguments: map[string]any{"path": "."}},
		{Name: "workspace.grep", Arguments: map[string]any{"pattern": "red", "path": "."}},
	} {
		invocation, err := runner.InvocationFromCall("run_tools", 0, call)
		if err != nil {
			t.Fatal(err)
		}
		if invocation.Call.ID == "" || invocation.Call.Risk != ptools.RiskLow {
			t.Fatalf("expected low risk invocation with generated id, got %#v", invocation.Call)
		}
	}
}

func TestToolRunnerAcceptsEditCall(t *testing.T) {
	runner := ToolRunner{}
	for _, call := range []ptools.Call{
		{Name: "workspace.edit_file", Arguments: map[string]any{"path": "note.txt", "old_text": "a", "new_text": "b"}},
		{Name: "workspace.apply_patch", Arguments: map[string]any{"patch": "--- a/note.txt\n+++ b/note.txt\n"}},
	} {
		invocation, err := runner.InvocationFromCall("run_tools", 0, call)
		if err != nil {
			t.Fatal(err)
		}
		if invocation.Call.Risk != ptools.RiskHigh {
			t.Fatalf("expected high risk invocation, got %#v", invocation.Call)
		}
	}
	diffInvocation, err := runner.InvocationFromCall("run_tools", 0, ptools.Call{
		Name:      "workspace.diff_file",
		Arguments: map[string]any{"path": "note.txt", "old_text": "a", "new_text": "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if diffInvocation.Call.Risk != ptools.RiskLow {
		t.Fatalf("expected diff tool to be low risk, got %#v", diffInvocation.Call)
	}
}

func TestToolRunnerRunsListSlashCommand(t *testing.T) {
	t.Setenv("RED_PANDA_SLASH_TOOLS", "1")
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "README.md"), "red panda\n")

	runner := ToolRunner{}
	invocation, ok := runner.Parse("/list .", "run_list")
	if !ok {
		t.Fatal("expected /list to parse")
	}
	result, output := runner.Run(context.Background(), root, invocation)
	if result.Status != ptools.CallStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if !strings.Contains(output, "README.md") {
		t.Fatalf("expected output to include README.md, got:\n%s", output)
	}
}

func TestToolRunnerSlashParseDisabledByDefault(t *testing.T) {
	t.Setenv("RED_PANDA_SLASH_TOOLS", "")
	runner := ToolRunner{}
	if _, ok := runner.Parse("/list .", "run_list"); ok {
		t.Fatal("slash tools must be off by default")
	}
}

func TestStateStoreToolDescriptionsClarifyBoundaries(t *testing.T) {
	byName := map[string]string{}
	for _, def := range (ToolRunner{}).AvailableTools() {
		byName[def.Name] = def.Description
	}
	checks := map[string][]string{
		"memory.create":   {"durable", "todo.write", "context.write"},
		"todo.write":      {"Session checklist", "memory", "context"},
		"goal.checkpoint": {"context.write", "memory.create"},
		"context.write":   {"scratchpad", "goal.checkpoint", "memory.create"},
	}
	for name, needles := range checks {
		desc := byName[name]
		if desc == "" {
			t.Fatalf("missing tool %s", name)
		}
		for _, n := range needles {
			if !strings.Contains(desc, n) {
				t.Fatalf("%s description missing %q: %s", name, n, desc)
			}
		}
	}
}

func TestToolRunnerMemoryToolsUseExecutor(t *testing.T) {
	var captured methods.MemoryToolExecuteParams
	runner := ToolRunner{
		MemoryExecutor: func(ctx context.Context, params methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
			captured = params
			return methods.MemoryToolExecuteResult{
				Status:   "completed",
				Output:   `{"action":"memory.create"}`,
				RecordID: "mem_1",
			}, nil
		},
	}

	listInvocation, err := runner.InvocationFromCall("run_memory_tool", 0, ptools.Call{
		Name:      "memory.list",
		Arguments: map[string]any{"scope": "session"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if listInvocation.Call.Risk != ptools.RiskMedium {
		t.Fatalf("memory.list risk = %s, want medium", listInvocation.Call.Risk)
	}

	createInvocation, err := runner.InvocationFromCall("run_memory_tool", 1, ptools.Call{
		Name:      "memory.create",
		Arguments: map[string]any{"scope": "session", "content": "remember this"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if createInvocation.Call.Risk != ptools.RiskHigh {
		t.Fatalf("memory.create risk = %s, want high", createInvocation.Call.Risk)
	}

	result, output := runner.RunWithContext(context.Background(), ToolRunContext{
		WorkingDir: "D:/workspace",
		RunID:      "run_memory_tool",
		SessionID:  "session_memory_tool",
	}, createInvocation)
	if result.Status != ptools.CallStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if !strings.Contains(output, "memory.create") {
		t.Fatalf("unexpected output: %s", output)
	}
	if captured.RunID != "run_memory_tool" || captured.SessionID != "session_memory_tool" || captured.WorkspaceRoot != "D:/workspace" {
		t.Fatalf("executor params mismatch: %#v", captured)
	}
	if captured.ToolName != "memory.create" || captured.ToolCallID == "" {
		t.Fatalf("executor tool identity mismatch: %#v", captured)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
