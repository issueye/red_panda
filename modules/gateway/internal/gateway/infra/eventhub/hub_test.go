package eventhub

import (
	"testing"
	"time"

	"redpanda/protocol/events"
)

func TestHubPublishesToRunSubscribers(t *testing.T) {
	hub := New()
	ch, cancel := hub.Subscribe("run_1")
	defer cancel()

	hub.Publish(events.EnvelopeV2{
		EventID: "evt_1",
		RunID:   "run_1",
		RunSeq:  1,
		Type:    events.EventMessageDelta,
	})

	select {
	case event := <-ch:
		if event.EventID != "evt_1" {
			t.Fatalf("event id = %q, want evt_1", event.EventID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestHubDoesNotPublishToOtherRuns(t *testing.T) {
	hub := New()
	ch, cancel := hub.Subscribe("run_1")
	defer cancel()

	hub.Publish(events.EnvelopeV2{
		EventID: "evt_2",
		RunID:   "run_2",
		RunSeq:  1,
		Type:    events.EventMessageDelta,
	})

	select {
	case event := <-ch:
		t.Fatalf("unexpected event: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}
