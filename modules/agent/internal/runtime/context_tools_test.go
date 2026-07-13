package runtime

import (
	"context"
	"encoding/json"
	"io"
	"redpanda/agent/internal/subagent"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// TestContextToolsRegisteredAndNotDenylisted 验证关键设计：context.* 工具具有正确风险等级，
// 且被有意排除在 subagent.RunDenylist 外，使专业子代理可共享暂存区笔记。
func TestContextToolsRegisteredAndNotDenylisted(t *testing.T) {
	runner := ToolRunner{}
	wantRisk := map[string]tools.Risk{
		"context.read":    tools.RiskLow,
		"context.search":  tools.RiskLow,
		"context.write":   tools.RiskHigh,
		"context.replace": tools.RiskHigh,
		"context.delete":  tools.RiskHigh,
	}
	for name, risk := range wantRisk {
		invocation, err := runner.InvocationFromCall("run_ctx", 0, tools.Call{
			Name:      name,
			Arguments: map[string]any{"goal_id": "g1", "query": "x", "kind": "finding", "title": "t", "body": "b", "note_id": "n1"},
		})
		if err != nil {
			t.Fatalf("%s did not resolve: %v", name, err)
		}
		if invocation.Call.Risk != risk {
			t.Fatalf("%s risk = %s, want %s", name, invocation.Call.Risk, risk)
		}
	}

	// 关键断言：子代理拒绝列表不得出现 context.* 项，否则专业子代理无法读写共享笔记。
	for _, denied := range subagent.RunDenylist {
		if len(denied) >= 8 && denied[:8] == "context." {
			t.Fatalf("context tool must not be denylisted for subagents: %q", denied)
		}
	}
}

func TestFetchGoalNotesRequiresSessionID(t *testing.T) {
	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	if notes := rt.fetchGoalNotes("run1", "", "goal1", 10); notes != nil {
		t.Fatalf("empty session must short-circuit without gateway call, got %#v", notes)
	}
	if notes := rt.fetchGoalNotes("run1", "sess1", "", 10); notes != nil {
		t.Fatalf("empty goal must short-circuit, got %#v", notes)
	}
}

func TestFetchGoalNotesPassesSessionIDAndReturnsNotes(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()

	type callResult struct {
		notes []methods.GoalNoteDTO
	}
	done := make(chan callResult, 1)
	go func() {
		notes := rt.fetchGoalNotes("run_inject", "sess_inject", "goal_inject", 5)
		done <- callResult{notes: notes}
	}()

	var outbound jsonrpc.Request
	if err := json.NewDecoder(reader).Decode(&outbound); err != nil {
		t.Fatal(err)
	}
	if outbound.Method != methods.ContextToolExecute {
		t.Fatalf("method = %s, want %s", outbound.Method, methods.ContextToolExecute)
	}
	var params methods.ContextToolExecuteParams
	if err := json.Unmarshal(outbound.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.SessionID != "sess_inject" {
		t.Fatalf("session_id = %q, want sess_inject (auto-inject must not send empty session)", params.SessionID)
	}
	if params.RunID != "run_inject" || params.ToolName != "context.read" {
		t.Fatalf("unexpected params: %#v", params)
	}
	if params.Arguments["goal_id"] != "goal_inject" {
		t.Fatalf("goal_id = %#v", params.Arguments["goal_id"])
	}
	// JSON 数字会被解码为 float64。
	limitVal, ok := params.Arguments["limit"].(float64)
	if !ok || limitVal != 5 {
		t.Fatalf("limit = %#v, want 5", params.Arguments["limit"])
	}

	response, err := jsonrpc.NewResult(outbound.ID, methods.ContextToolExecuteResult{
		Status: "completed",
		Notes: []methods.GoalNoteDTO{
			{ID: "n1", GoalID: "goal_inject", Kind: "finding", Title: "Found bug", Body: "nil deref", Pinned: 1, Seq: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rawResponse, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.handleLine(ctx, rawResponse); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-done:
		if len(result.notes) != 1 || result.notes[0].Title != "Found bug" {
			t.Fatalf("notes = %#v", result.notes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fetchGoalNotes")
	}
}

func TestGoalNotesDigestAndBriefIncludeFetchedNotes(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()

	type digests struct {
		digest string
		brief  string
	}
	done := make(chan digests, 1)
	go func() {
		// 两次顺序 Gateway 调用：先获取摘要，再获取简报。
		d := rt.goalNotesDigest("run_d", "sess_d", "goal_d")
		b := rt.goalNotesBrief("run_d", "sess_d", "goal_d", "Ship feature X")
		done <- digests{digest: d, brief: b}
	}()

	for i := 0; i < 2; i++ {
		var outbound jsonrpc.Request
		if err := json.NewDecoder(reader).Decode(&outbound); err != nil {
			t.Fatal(err)
		}
		response, err := jsonrpc.NewResult(outbound.ID, methods.ContextToolExecuteResult{
			Status: "completed",
			Notes: []methods.GoalNoteDTO{
				{ID: "n1", Kind: "decision", Title: "Use SQLite", Body: "pure go driver", Pinned: 1, Seq: 1},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.handleLine(ctx, raw); err != nil {
			t.Fatal(err)
		}
	}

	select {
	case result := <-done:
		if !strings.Contains(result.digest, "Shared goal notes") || !strings.Contains(result.digest, "Use SQLite") {
			t.Fatalf("digest missing notes: %q", result.digest)
		}
		if !strings.Contains(result.brief, "goal_d") || !strings.Contains(result.brief, "Ship feature X") {
			t.Fatalf("brief missing goal header: %q", result.brief)
		}
		if !strings.Contains(result.brief, "Use SQLite") {
			t.Fatalf("brief missing note: %q", result.brief)
		}
		if strings.Contains(result.brief, "no shared notes yet") {
			t.Fatalf("brief should not claim empty when notes returned: %q", result.brief)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for digest/brief")
	}
}

func TestRenderNotesDigestPinnedAndTruncation(t *testing.T) {
	longBody := strings.Repeat("x", goalNoteDigestBodyRune+50)
	out := renderNotesDigest("Shared", []methods.GoalNoteDTO{
		{Kind: "finding", Title: "A", Body: longBody, Pinned: 1},
		{Kind: "note", Title: "", Body: "short"},
	})
	if !strings.Contains(out, "[pinned]") {
		t.Fatalf("expected pinned marker: %q", out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("expected truncated body: %q", out)
	}
	if !strings.Contains(out, "(untitled)") {
		t.Fatalf("expected untitled fallback: %q", out)
	}
}
