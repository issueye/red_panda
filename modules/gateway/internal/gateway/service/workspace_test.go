package service

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/methods"
)

func TestWorkspaceRemoveUsesSessionLifecycle(t *testing.T) {
	repos, _ := newSessionServiceTestFixture(t)
	hub := eventhub.New()
	events, cancel := hub.SubscribeBroadcast()
	defer cancel()
	workspace, err := repos.Workspaces.Upsert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var sessionIDs []string
	for index := 0; index < 2; index++ {
		session, err := repos.Sessions.Create("workspace-session", workspace.Root)
		if err != nil {
			t.Fatal(err)
		}
		sessionIDs = append(sessionIDs, session.ID)
		if err := repos.Runs.Start(model.RunRecord{ID: "run-" + session.ID, SessionID: session.ID, Status: "running"}); err != nil {
			t.Fatal(err)
		}
		if _, err := repos.Schedules.Create(model.ScheduledTask{
			ID: "schedule-" + session.ID, Name: "fixed", SessionID: session.ID,
			SessionMode: methods.ScheduleSessionFixed, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := repos.Todos.SaveAll([]model.TodoItem{{ID: "todo-" + session.ID, SessionID: session.ID, Content: "pending", Status: "pending"}}); err != nil {
			t.Fatal(err)
		}
	}
	lifecycle := newPurgeService(repos, nil, hub, t.TempDir())
	service := NewWorkspaceService(repos, lifecycle)
	_, deleted, err := service.Remove(workspace.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted sessions = %d, want 2", deleted)
	}
	deletedEvents := map[string]string{}
	for range sessionIDs {
		select {
		case event := <-events:
			if event.Method != "session.deleted" {
				t.Fatalf("event method = %s", event.Method)
			}
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			deletedEvents[payload["id"].(string)] = payload["reason"].(string)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for session.deleted")
		}
	}
	for _, sessionID := range sessionIDs {
		if _, err := repos.Sessions.Get(sessionID); err != gorm.ErrRecordNotFound {
			t.Fatalf("session %s still active: %v", sessionID, err)
		}
		// docs/49: hard-delete removes runs/todos; schedules are disabled only.
		if _, err := repos.Runs.Get("run-" + sessionID); err != gorm.ErrRecordNotFound {
			t.Fatalf("run should be hard-deleted: %v", err)
		}
		schedule, err := repos.Schedules.Get("schedule-" + sessionID)
		if err != nil || schedule.Enabled || schedule.NextRunAt != nil {
			t.Fatalf("schedule after delete = %#v, %v", schedule, err)
		}
		todos, err := repos.Todos.ListBySession(sessionID)
		if err != nil || len(todos) != 0 {
			t.Fatalf("todos after delete = %#v, %v", todos, err)
		}
		if deletedEvents[sessionID] != "workspace_delete" {
			t.Fatalf("missing delete event for %s: %#v", sessionID, deletedEvents)
		}
	}
}
