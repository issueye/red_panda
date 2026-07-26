package methods

import "testing"

func TestCanonicalToolNameNoAliases(t *testing.T) {
	t.Parallel()
	// Cutover: aliases are no longer rewritten.
	if got := CanonicalToolName("todo_write"); got != "todo_write" {
		t.Fatalf("CanonicalToolName(todo_write) = %q, want identity (rejected elsewhere)", got)
	}
	if got := CanonicalToolName(" todo.write "); got != "todo.write" {
		t.Fatalf("CanonicalToolName trim = %q", got)
	}
}

func TestIsTodoWriteTool(t *testing.T) {
	t.Parallel()
	if !IsTodoWriteTool("todo.write") {
		t.Fatal("todo.write should match")
	}
	if IsTodoWriteTool("todo_write") {
		t.Fatal("todo_write alias removed")
	}
	if IsTodoWriteTool("todo.list") {
		t.Fatal("list is not write")
	}
}

func TestIsRemovedToolAlias(t *testing.T) {
	t.Parallel()
	if !IsRemovedToolAlias("todo_write") || !IsRemovedToolAlias("segment_end") {
		t.Fatal("expected removed aliases")
	}
	if IsRemovedToolAlias("todo.write") {
		t.Fatal("canonical names are not removed aliases")
	}
}
