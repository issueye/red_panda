package runtime

import (
	"context"
	"fmt"

	"redpanda/agent/internal/subagent"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

type runtimeProcessProvider struct {
	runtime *Runtime
}

func (p runtimeProcessProvider) Acquire(ctx context.Context, params methods.ReplyParams, subAgentID string, backend string) (subagent.Process, subagent.ReleaseFunc, error) {
	if p.runtime == nil {
		return nil, nil, fmt.Errorf("runtime is not available")
	}
	switch backend {
	case "process_pool":
		child, release, err := p.runtime.processPool.Acquire(ctx, params, subAgentID)
		if err != nil {
			return nil, nil, err
		}
		return child, subagent.ReleaseFunc(release), nil
	case "runtime_process":
		child, err := p.runtime.newProcessSubAgent(ctx, params, subAgentID)
		if err != nil {
			return nil, nil, err
		}
		return child, func(bool) {
			_ = child.Close(context.Background())
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported subagent backend %q", backend)
	}
}

type runtimeEventSink struct {
	runtime *Runtime
}

func (s runtimeEventSink) EmitStatus(ctx context.Context, spec subagent.RunSpec, status subagent.StatusEvent) error {
	if s.runtime == nil {
		return fmt.Errorf("runtime is not available")
	}
	payload := map[string]any{
		"subagent_id":     spec.SubAgentID,
		"name":            spec.Name,
		"display_name":    spec.DisplayName,
		"status":          status.Status,
		"summary":         status.Summary,
		"backend":         spec.Backend,
		"task":            spec.Task,
		"file_count":      spec.FileCount,
		"max_turns":       spec.MaxTurns,
		"path":            spec.ScopePath,
		"goal_specialist": spec.GoalPhase != "",
		"goal_phase":      spec.GoalPhase,
	}
	if status.Error != "" {
		payload["error"] = status.Error
	}
	return s.runtime.emitAgentEvent(ctx, spec.Parent, subAgentRef(spec.SubAgentID, spec.Name), events.EventSubAgentUpdate, nil, payload)
}

func (s runtimeEventSink) Bridge(ctx context.Context, spec subagent.RunSpec, child events.Envelope) error {
	if s.runtime == nil {
		return fmt.Errorf("runtime is not available")
	}
	if child.Type == events.EventFinish {
		return nil
	}
	payload := copyPayload(child.Payload)
	payload["subagent_id"] = spec.SubAgentID
	payload["backend"] = spec.Backend
	if child.Type == events.EventSubAgentUpdate {
		payload["name"] = firstPayloadString(payload, "name", spec.Name)
	}
	stream := child.Stream
	if stream != nil {
		next := *stream
		next.StreamID = "stream_" + spec.SubAgentID + "_" + stream.StreamID
		stream = &next
	}
	return s.runtime.emitAgentEvent(ctx, spec.Parent, subAgentRef(spec.SubAgentID, spec.Name), child.Type, stream, payload)
}
