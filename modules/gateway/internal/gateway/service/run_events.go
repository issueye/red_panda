package service

import (
	"encoding/json"
	"fmt"
	"time"

	"redpanda/protocol/events"
)

// Runtime event projection and process-exit recovery.
func (r RunService) HandleRuntimeEvent(event events.EnvelopeV2) {
	if event.EventID == "" || event.RunID == "" {
		return
	}
	_ = r.repos.Runs.ProjectEvent(event)
	if event.Type == events.EventPermissionRequest {
		_ = r.repos.Permissions.ProjectRequired(event)
	}
	if isToolEvent(event.Type) {
		_ = r.repos.ToolCalls.Project(event)
		_ = r.repos.Runs.RefreshToolCount(event.RunID)
	}
	if event.Type == events.EventFinish || event.Type == events.EventError {
		_ = r.repos.Permissions.ClosePendingByRun(event.RunID, "closed", "run finished")
		status := "completed"
		if event.Type == events.EventError {
			status = "failed"
		} else if s, ok := event.Payload["status"].(string); ok && s != "" {
			status = s
		}
		if reason, _ := event.Payload["loop_end_reason"].(string); reason == "budget_exhausted" {
			status = "budget_exhausted"
		}
		errText, _ := event.Payload["message"].(string)
		if errText == "" {
			errText, _ = event.Payload["error"].(string)
		}
		NewScheduleService(r.repos, r.hub, nil).OnRunTerminal(event.RunID, status, errText)
	}
	_ = r.repos.RunEvents.Save(event)
	messageRole := ""
	switch event.Type {
	case events.EventMessageDelta:
		messageRole = "assistant"
	case events.EventReasoningDelta:
		messageRole = "reasoning"
	}
	if messageRole != "" && payloadString(event.Payload, "visibility") != "worker_private" {
		if delta, ok := event.Payload["delta"].(string); ok && delta != "" {
			metadata, _ := json.Marshal(map[string]any{
				"assignment_id": event.AssignmentID,
				"worker_id":     event.Worker.ID,
				"profile_key":   event.Worker.ProfileKey,
				"visibility":    payloadString(event.Payload, "visibility"),
			})
			_, _ = r.repos.Messages.AddOrAppendWithMetadata(event.SessionID, messageRole, delta, event.RunID, string(metadata))
			_ = r.repos.Sessions.Touch(event.SessionID)
		}
	}
	r.hub.Publish(event)
}

// HandleRuntimeExit closes a run whose dedicated agent process disappeared
// without sending EventFinish/EventError. Without this, its persisted "running"
// record permanently consumes a global concurrency slot until gateway restart.
func (r RunService) HandleRuntimeExit(runID string, exitErr error) {
	row, err := r.repos.Runs.Get(runID)
	if err != nil || (row.Status != "running" && row.Status != "waiting_permission") {
		return
	}
	message := "dedicated runtime process exited before the run finished"
	if exitErr != nil {
		message = fmt.Sprintf("%s: %v", message, exitErr)
	}
	r.HandleRuntimeEvent(events.EnvelopeV2{
		ProtocolVersion: events.ProtocolVersionV2,
		EventID:         fmt.Sprintf("evt_runtime_exit_%d", time.Now().UnixNano()),
		RunID:           row.ID,
		SessionID:       row.SessionID,
		RunSeq:          row.LastRootSeq + 1,
		Type:            events.EventError,
		Payload: map[string]any{
			"status":  "failed",
			"message": message,
		},
		CreatedAt: time.Now().UTC(),
	})
}

func isToolEvent(typ events.EventType) bool {
	return typ == events.EventToolStarted ||
		typ == events.EventToolOutput ||
		typ == events.EventToolFinished ||
		typ == events.EventToolFailed
}
