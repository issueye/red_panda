package worker

import (
	"testing"

	"redpanda/protocol/events"
)

func TestCaptureKeepsOnlyFinalNoToolResponseAndStats(t *testing.T) {
	capture := NewCapture(CaptureOptions{MaxTurns: 5})
	capture.ObserveV2(events.EnvelopeV2{
		Type:    events.EventMessageDelta,
		Stream:  &events.StreamRef{Kind: events.StreamMessage, Final: true},
		Payload: map[string]any{"delta": "I will inspect the files first."},
	})
	capture.ObserveV2(events.EnvelopeV2{
		Type:    events.EventToolStarted,
		Payload: map[string]any{"tool_name": "workspace.read_files"},
	})
	capture.ObserveV2(events.EnvelopeV2{Type: events.EventToolFinished})
	capture.ObserveV2(events.EnvelopeV2{
		Type:    events.EventMessageDelta,
		Stream:  &events.StreamRef{Kind: events.StreamMessage, Final: true},
		Payload: map[string]any{"delta": "Final report only."},
	})
	capture.ObserveV2(events.EnvelopeV2{Type: events.EventFinish, Payload: map[string]any{
		"status": "completed", "max_turns": 5.0, "loop_turns": 2.0,
		"provider_requests": 3.0, "tool_calls_requested": 1.0,
		"tool_calls_executed": 1.0, "tool_call_budget": 20.0,
	}})

	if got := capture.FinalText(); got != "Final report only." {
		t.Fatalf("FinalText = %q", got)
	}
	stats := capture.Stats()
	if stats.MaxTurns != 5 || stats.LoopTurns != 2 || stats.ProviderRequests != 3 || stats.ToolCallsExecuted != 1 {
		t.Fatalf("Stats = %+v", stats)
	}
}

func TestCaptureUsesFinishFallbackForLegacyEvents(t *testing.T) {
	capture := NewCapture(CaptureOptions{MaxTurns: 3})
	capture.ObserveV2(events.EnvelopeV2{Type: events.EventFinish, Payload: map[string]any{
		"status": "completed", "text": "legacy final report",
	}})
	if got := capture.FinalText(); got != "legacy final report" {
		t.Fatalf("FinalText = %q", got)
	}
	if capture.Stats().MaxTurns != 3 {
		t.Fatalf("Stats = %+v", capture.Stats())
	}
}
