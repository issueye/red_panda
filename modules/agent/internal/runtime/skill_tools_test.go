package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	"redpanda/protocol/tools"
)

type skillCreateProvider struct{}

func (skillCreateProvider) Name() string {
	return "skill-create-test"
}

func (skillCreateProvider) Complete(_ context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	if len(req.ToolHistory) == 0 {
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:   "call_permission_skill_create",
			Name: "skill.create",
			Arguments: map[string]any{
				"name":         "permission-review",
				"description":  "Review permission behavior.",
				"instructions": "Do not create this skill when permission is denied.",
			},
		}}})
	}
	if err := emit(ProviderChunk{Delta: "unexpected"}); err != nil {
		return err
	}
	return emit(ProviderChunk{Final: true})
}

func TestManagedSkillCreateAndUpdate(t *testing.T) {
	root := t.TempDir()
	createOutput, err := runCreateSkill(root, "code-review", "Review code safely.", "# Workflow\n\n1. Inspect the diff.\n2. Report findings.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(createOutput, `"action":"skill.create"`) || !strings.Contains(createOutput, `.codex/skills/code-review/SKILL.md`) {
		t.Fatalf("unexpected create output: %s", createOutput)
	}
	target := filepath.Join(root, ".codex", "skills", "code-review", "SKILL.md")
	assertManagedSkillContent(t, target, "code-review", "Review code safely.", "1. Inspect the diff.")

	updateOutput, err := runUpdateSkill(root, "code-review", "Review code and tests.", "# Workflow\n\n1. Inspect changes.\n2. Run focused tests.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updateOutput, `"action":"skill.update"`) {
		t.Fatalf("unexpected update output: %s", updateOutput)
	}
	assertManagedSkillContent(t, target, "code-review", "Review code and tests.", "2. Run focused tests.")
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".SKILL.md-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary skill files remain: %v", matches)
	}
}

func TestManagedSkillCreateAndUpdateRejectInvalidState(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", "Code-Review", "../escape", "nested/review", "review_tool"} {
		if _, err := runCreateSkill(root, name, "description", "instructions"); err == nil {
			t.Fatalf("create accepted invalid name %q", name)
		}
	}
	if _, err := runCreateSkill(root, "review", "", "instructions"); err == nil {
		t.Fatal("create accepted an empty description")
	}
	if _, err := runCreateSkill(root, "review", "description", ""); err == nil {
		t.Fatal("create accepted empty instructions")
	}

	if _, err := runCreateSkill(root, "review", "first description", "first instructions"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ".codex", "skills", "review", "SKILL.md")
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runCreateSkill(root, "review", "replacement", "replacement"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate create error = %v", err)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("duplicate create changed the existing skill")
	}

	if _, err := runUpdateSkill(root, "missing", "description", "instructions"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing update error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "skills", "missing", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("missing update created a file: %v", err)
	}
}

func TestManagedSkillCreateRejectsEscapingSymlinkWithoutOutsideWrites(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".codex")); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	if _, err := runCreateSkill(root, "review", "description", "instructions"); err == nil || !strings.Contains(err.Error(), "escapes managed skill root") {
		t.Fatalf("escaping symlink error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "skills")); !os.IsNotExist(err) {
		t.Fatalf("escaping symlink created an outside directory: %v", err)
	}
}

func TestToolRunnerRegistersManagedSkillToolsAsHighRisk(t *testing.T) {
	runner := ToolRunner{}
	for index, name := range []string{"skill.create", "skill.update", "skill.delete", "skill.run"} {
		arguments := map[string]any{
			"name":         "review",
			"description":  "Review changes.",
			"instructions": "Inspect the diff.",
		}
		if name == "skill.run" {
			arguments = map[string]any{"name": "review", "task": "inspect the diff"}
		}
		if name == "skill.delete" {
			arguments = map[string]any{"name": "review"}
		}
		invocation, err := runner.InvocationFromCall("run_skill", index, tools.Call{
			Name:      name,
			Arguments: arguments,
		})
		if err != nil {
			t.Fatal(err)
		}
		if invocation.Call.ID == "" || invocation.Call.Risk != tools.RiskHigh || invocation.Call.DisplayName == "" {
			t.Fatalf("unexpected %s invocation: %#v", name, invocation.Call)
		}
	}

	listInvocation, err := runner.InvocationFromCall("run_skill_list", 0, tools.Call{Name: "skill.list"})
	if err != nil {
		t.Fatal(err)
	}
	if listInvocation.Call.Risk != tools.RiskLow {
		t.Fatalf("skill.list risk = %s, want low", listInvocation.Call.Risk)
	}
}

func TestManagedSkillListLoadDelete(t *testing.T) {
	root := t.TempDir()
	emptyList, err := runListSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emptyList, `"count":0`) {
		t.Fatalf("empty list = %s", emptyList)
	}

	if _, err := runCreateSkill(root, "alpha", "Alpha skill.", "# Alpha\n\nDo alpha."); err != nil {
		t.Fatal(err)
	}
	if _, err := runCreateSkill(root, "beta", "Beta skill.", "# Beta\n\nDo beta."); err != nil {
		t.Fatal(err)
	}

	listOutput, err := runListSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOutput, `"count":2`) || !strings.Contains(listOutput, `"name":"alpha"`) || !strings.Contains(listOutput, `"name":"beta"`) {
		t.Fatalf("list output = %s", listOutput)
	}
	if strings.Contains(listOutput, "Do alpha.") || strings.Contains(listOutput, "Do beta.") {
		t.Fatalf("list output leaked instructions: %s", listOutput)
	}

	detail, err := loadManagedSkillDetail(root, "alpha", true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Name != "alpha" || detail.Description != "Alpha skill." || !strings.Contains(detail.Instructions, "Do alpha.") {
		t.Fatalf("unexpected detail: %#v", detail)
	}
	summaryOnly, err := loadManagedSkillDetail(root, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if summaryOnly.Instructions != "" {
		t.Fatalf("summary load included instructions: %#v", summaryOnly)
	}

	deleteOutput, err := runDeleteSkill(root, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(deleteOutput, `"deleted":true`) {
		t.Fatalf("delete output = %s", deleteOutput)
	}
	if _, err := loadManagedSkillDetail(root, "alpha", false); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("deleted skill still loadable: %v", err)
	}
	if _, err := runDeleteSkill(root, "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing delete error = %v", err)
	}
}

func TestAgentSkillsJSONRPCHandlers(t *testing.T) {
	root := t.TempDir()
	if _, err := runCreateSkill(root, "review", "Review code.", "# Steps\n\n1. Read."); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	lines := make(chan []byte, 16)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	sendRequest(t, context.Background(), rt, "skills_list", methods.AgentSkills, methods.SkillsListParams{
		WorkspaceRoot: root,
	})
	listResp := waitForResponse(t, lines, "skills_list")
	var listResult methods.SkillsListResult
	if err := json.Unmarshal(listResp.Result, &listResult); err != nil {
		t.Fatal(err)
	}
	if len(listResult.Items) != 1 || listResult.Items[0].Name != "review" {
		t.Fatalf("list result = %#v", listResult)
	}

	sendRequest(t, context.Background(), rt, "skill_load", methods.AgentSkillLoad, methods.SkillLoadParams{
		WorkspaceRoot:       root,
		Name:                "review",
		IncludeInstructions: true,
	})
	loadResp := waitForResponse(t, lines, "skill_load")
	var loadResult methods.SkillLoadResult
	if err := json.Unmarshal(loadResp.Result, &loadResult); err != nil {
		t.Fatal(err)
	}
	if loadResult.Skill.Description != "Review code." || !strings.Contains(loadResult.Skill.Instructions, "1. Read.") {
		t.Fatalf("load result = %#v", loadResult)
	}

	sendRequest(t, context.Background(), rt, "skill_delete", methods.AgentSkillDelete, methods.SkillDeleteParams{
		WorkspaceRoot: root,
		Name:          "review",
	})
	deleteResp := waitForResponse(t, lines, "skill_delete")
	var deleteResult methods.SkillDeleteResult
	if err := json.Unmarshal(deleteResp.Result, &deleteResult); err != nil {
		t.Fatal(err)
	}
	if !deleteResult.Deleted {
		t.Fatalf("delete result = %#v", deleteResult)
	}
}

func TestRuntimeHTTPProviderCreatesManagedSkill(t *testing.T) {
	root := t.TempDir()
	var mu sync.Mutex
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, body)
		requestCount := len(requests)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"call_skill_create","type":"function","function":{"name":"skill__create","arguments":"{\"name\":\"review-helper\",\"description\":\"Review code changes.\",\"instructions\":\"# Workflow\\n\\n1. Inspect changes.\\n2. Report findings.\"}"}}]}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"SKILL_HTTP_OK"}}]}`)
	}))
	defer server.Close()

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = HTTPCompatibleProvider{baseURL: server.URL, model: "test-model", client: server.Client()}
	sendRequest(t, context.Background(), rt, "reply_skill_http", methods.AgentReply, methods.ReplyParams{
		RunID: "run_skill_http",
		Session: methods.ReplySession{
			ID:         "session_skill_http",
			WorkingDir: root,
		},
		Input: methods.ReplyInput{Text: "Create a review skill."},
		Options: methods.ReplyOptions{
			ToolPolicy:     "allow_all",
			PermissionMode: "strict",
		},
	})
	waitForResponse(t, lines, "reply_skill_http")
	runEvents := waitForEventsUntilFinish(t, lines)
	assertEventSequence(t, runEvents, []events.EventType{
		events.EventToolStarted,
		events.EventToolOutput,
		events.EventToolFinished,
		events.EventMessageDelta,
		events.EventFinish,
	})
	finish := runEvents[len(runEvents)-1]
	if finish.Payload["status"] != "completed" {
		t.Fatalf("finish = %#v, want completed", finish.Payload)
	}
	assertManagedSkillContent(t, filepath.Join(root, ".codex", "skills", "review-helper", "SKILL.md"), "review-helper", "Review code changes.", "2. Report findings.")

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider request count = %d, want 2", len(requests))
	}
	secondMessages, ok := requests[1]["messages"].([]any)
	if !ok {
		t.Fatalf("second request messages = %#v", requests[1]["messages"])
	}
	var sawToolResult bool
	for _, raw := range secondMessages {
		message, _ := raw.(map[string]any)
		if message["role"] == "tool" && message["name"] == "skill__create" {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Fatalf("second provider request did not contain skill tool result: %#v", secondMessages)
	}
}

func TestRuntimeManagedSkillCreateHonorsPermissionDenial(t *testing.T) {
	root := t.TempDir()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = skillCreateProvider{}
	sendRequest(t, context.Background(), rt, "reply_skill_permission", methods.AgentReply, methods.ReplyParams{
		RunID: "run_skill_permission",
		Session: methods.ReplySession{
			ID:         "session_skill_permission",
			WorkingDir: root,
		},
		Input: methods.ReplyInput{Text: "Create a skill."},
		Options: methods.ReplyOptions{
			ToolPolicy:     "risk_based",
			PermissionMode: "strict",
		},
	})
	waitForResponse(t, lines, "reply_skill_permission")
	permissionEvent := waitForEventType(t, lines, events.EventPermissionRequest)
	if permissionEvent.Payload["tool_name"] != "skill.create" || permissionEvent.Payload["risk"] != string(tools.RiskHigh) {
		t.Fatalf("permission event = %#v", permissionEvent.Payload)
	}
	permissionID, _ := permissionEvent.Payload["permission_id"].(string)
	if permissionID == "" {
		t.Fatalf("permission event missing permission_id: %#v", permissionEvent.Payload)
	}
	sendRequest(t, context.Background(), rt, "deny_skill_permission", methods.PermissionResolve, permission.ResolveParams{
		PermissionID: permissionID,
		RunID:        "run_skill_permission",
		Decision:     permission.DecisionDeny,
		Reason:       "test denial",
	})
	failed := waitForResponseAndEvent(t, lines, "deny_skill_permission", events.EventToolFailed)
	if failed.Payload["tool_name"] != "skill.create" || failed.Payload["status"] != string(tools.CallStatusDenied) {
		t.Fatalf("tool failed event = %#v", failed.Payload)
	}
	eventsAfterDenial := waitForEventsUntilFinish(t, lines)
	finish := eventsAfterDenial[len(eventsAfterDenial)-1]
	// Permission denial fails the tool but the provider loop continues so the model can recover.
	if finish.Payload["status"] != "completed" {
		t.Fatalf("finish = %#v, want completed after recoverable tool denial", finish.Payload)
	}
	for _, event := range eventsAfterDenial {
		if event.Type == events.EventError {
			t.Fatalf("did not expect run-level error after permission denial: %#v", event.Payload)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "skills", "permission-review", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("permission-denied skill was created: %v", err)
	}
}

func assertManagedSkillContent(t *testing.T, path string, name string, description string, instructionFragment string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, expected := range []string{"---\n", "name: " + name, `description: "` + description + `"`, instructionFragment} {
		if !strings.Contains(content, expected) {
			t.Fatalf("skill content missing %q:\n%s", expected, content)
		}
	}
}
