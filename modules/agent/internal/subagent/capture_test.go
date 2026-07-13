package subagent

import (
	"strings"
	"testing"

	"redpanda/protocol/events"
)

func TestCaptureBuildsActionableFailure(t *testing.T) {
	capture := NewCapture(CaptureOptions{
		MaxTurns: 16,
		Backend:  "process_pool",
		Name:     "desktop",
		Task:     "analyze desktop frontend modules",
	})
	capture.Observe(events.Envelope{
		Type: events.EventToolStarted,
		Payload: map[string]any{
			"tool_name": "workspace.read_file",
		},
	})
	capture.Observe(events.Envelope{
		Type: events.EventToolFailed,
		Payload: map[string]any{
			"tool_name": "workspace.read_file",
			"error":     "file not found",
		},
	})
	capture.Observe(events.Envelope{
		Type: events.EventError,
		Payload: map[string]any{
			"message": "provider exceeded tool turn limit (16)",
			"status":  "failed",
		},
	})

	err := capture.FailureError("subagent returned an empty final report")
	for _, want := range []string{
		"subagent returned an empty final report",
		"subagent=desktop",
		"backend=process_pool",
		"max_turns=16",
		"tools_failed=1",
		"file not found",
		"hint=",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q:\n%s", want, err)
		}
	}
}

func TestCaptureCollectsMessagesAndReasoning(t *testing.T) {
	capture := NewCapture(CaptureOptions{Name: "backend"})
	capture.Observe(events.Envelope{
		Type:   events.EventMessageDelta,
		Stream: &events.StreamRef{Kind: events.StreamMessage},
		Payload: map[string]any{
			"delta": "hello ",
		},
	})
	capture.Observe(events.Envelope{
		Type: events.EventMessage,
		Payload: map[string]any{
			"text": "from child",
		},
	})
	capture.Observe(events.Envelope{
		Type: events.EventReasoningDelta,
		Payload: map[string]any{
			"delta": "private reasoning",
		},
	})

	if got := capture.FinalText(); got != "hello from child" {
		t.Fatalf("FinalText = %q", got)
	}
}

func TestCaptureTracksRecoveryAndFinishFallback(t *testing.T) {
	capture := NewCapture(CaptureOptions{Name: "goal-analyst"})
	capture.Observe(events.Envelope{
		Type: events.EventMessageDelta,
		Payload: map[string]any{
			"delta":     "recovered result",
			"recovered": true,
		},
	})
	if !capture.RecoveredFallback() {
		t.Fatal("expected recovered fallback to be tracked")
	}

	fallback := NewCapture(CaptureOptions{Name: "worker"})
	fallback.Observe(events.Envelope{
		Type: events.EventFinish,
		Payload: map[string]any{
			"status": "completed",
			"text":   "finish-only result",
		},
	})
	if got := fallback.FinishStatus(); got != "completed" {
		t.Fatalf("FinishStatus = %q", got)
	}
	if got := fallback.FinalText(); got != "finish-only result" {
		t.Fatalf("FinalText = %q", got)
	}
}
