package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

// Event emission, sequencing, and JSON-RPC write helpers.
func (r *Runtime) emitEvent(ctx context.Context, params methods.ReplyParams, typ events.EventType, stream *events.StreamRef, payload map[string]any) error {
	return r.emitAgentEvent(ctx, params, events.AgentRef{
		AgentID: "root",
		Role:    events.AgentRoleRoot,
		Path:    []string{"root"},
		Name:    "root",
	}, typ, stream, payload)
}

// emitAgentEvent emits a v0.2 Worker-scoped event using EnvelopeV2.
// The agent parameter is kept for minimal internal compatibility (Role/Name/Path) but hierarchy is ignored.
func (r *Runtime) emitAgentEvent(ctx context.Context, params methods.ReplyParams, agent events.AgentRef, typ events.EventType, stream *events.StreamRef, payload map[string]any) error {
	if err := r.runStates.WaitIfPaused(ctx, params.RunID); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	r.eventMu.Lock()
	defer r.eventMu.Unlock()

	rootSeq := r.nextRootSeq(params.RunID)

	// v0.2+: ALWAYS emit EnvelopeV2 via RunEvent. No legacy root/subagent hierarchy.
	assignment := assignmentFromContext(ctx)
	assignmentID := string(assignment.AssignmentID)
	workerID := string(assignment.WorkerID)
	profileKey := agent.Name
	if params.Options.WorkerContext != nil {
		if assignmentID == "" {
			assignmentID = params.Options.WorkerContext.AssignmentID
		}
		if workerID == "" {
			workerID = params.Options.WorkerContext.WorkerID
		}
	}
	if workerID == "" {
		workerID = "worker-unassigned"
	}
	workerSeq := r.nextAgentSeq(params.RunID, workerID)
	body := cloneEventPayload(payload)
	if _, exists := body["visibility"]; !exists {
		body["visibility"] = "conversation"
	}
	env := events.EnvelopeV2{
		ProtocolVersion: events.ProtocolVersionV2,
		EventID:         fmt.Sprintf("evt_%s_%d", params.RunID, rootSeq),
		RunID:           params.RunID,
		SessionID:       params.Session.ID,
		AssignmentID:    assignmentID,
		Worker:          events.EventWorkerRef{ID: workerID, ProfileKey: profileKey},
		RunSeq:          rootSeq,
		WorkerSeq:       workerSeq,
		Stream:          stream,
		Type:            typ,
		Payload:         body,
		CreatedAt:       time.Now().UTC(),
	}
	note, err := jsonrpc.NewNotification(methods.RunEvent, env)
	if err != nil {
		return err
	}
	return r.writeNotification(note)
}

func cloneEventPayload(payload map[string]any) map[string]any {
	next := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		next[key] = value
	}
	return next
}

func (r *Runtime) nextRootSeq(runID string) uint64 {
	return r.runStates.NextRunSeq(runID)
}

func (r *Runtime) nextAgentSeq(runID string, agentID string) uint64 {
	return r.runStates.NextWorkerSeq(runID, agentID)
}

func (r *Runtime) writeResponse(resp jsonrpc.Response) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err = fmt.Fprintln(r.out, string(raw))
	return err
}

func (r *Runtime) writeNotification(note jsonrpc.Notification) error {
	raw, err := json.Marshal(note)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err = fmt.Fprintln(r.out, string(raw))
	return err
}
