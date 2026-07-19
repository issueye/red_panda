package methods

import "testing"

func TestCanonicalToolName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"todo_write", "todo.write"},
		{" todo_write ", "todo.write"},
		{"todo.write", "todo.write"},
		{"todo.list", "todo.list"},
		{"goal.plan", "goal.plan"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := CanonicalToolName(tc.in); got != tc.want {
			t.Fatalf("CanonicalToolName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsLegacyToolAlias(t *testing.T) {
	t.Parallel()
	if !IsLegacyToolAlias("todo_write") {
		t.Fatal("todo_write should be legacy")
	}
	if IsLegacyToolAlias("todo.write") {
		t.Fatal("todo.write is canonical")
	}
	if IsLegacyToolAlias("") {
		t.Fatal("empty is not legacy")
	}
}

func TestIsInternalGoalBudgetTool(t *testing.T) {
	t.Parallel()
	if !IsInternalGoalBudgetTool(InternalGoalSegmentEnd) {
		t.Fatal("segment_end is internal budget tool")
	}
	if IsInternalGoalBudgetTool("goal.assess") {
		t.Fatal("goal.assess is model-facing")
	}
}

func TestIsTodoWriteTool(t *testing.T) {
	t.Parallel()
	if !IsTodoWriteTool("todo_write") || !IsTodoWriteTool("todo.write") {
		t.Fatal("both aliases should count as todo write")
	}
	if IsTodoWriteTool("todo.list") {
		t.Fatal("list is not write")
	}
}
