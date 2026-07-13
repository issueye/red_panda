package events

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEnvelopeV2JSONRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 7, 13, 12, 30, 0, 0, time.UTC)
	want := EnvelopeV2{
		ProtocolVersion: ProtocolVersionV2,
		EventID:         "evt_run_123_42",
		RunID:           "run_123",
		SessionID:       "session_789",
		AssignmentID:    "assignment_456",
		Worker:          EventWorkerRef{ID: "worker-02", ProfileKey: "goal-verifier"},
		RunSeq:          42,
		WorkerSeq:       7,
		Type:            EventWorkerAssignmentUpdated,
		Payload:         map[string]any{"status": "running", "summary": "verifying changes"},
		CreatedAt:       createdAt,
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal EnvelopeV2: %v", err)
	}

	var got EnvelopeV2
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal EnvelopeV2: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEventWorkerRefIsMinimal(t *testing.T) {
	raw, err := json.Marshal(EventWorkerRef{ID: "worker-02", ProfileKey: "goal-verifier"})
	if err != nil {
		t.Fatalf("marshal EventWorkerRef: %v", err)
	}
	if string(raw) != `{"id":"worker-02","profile_key":"goal-verifier"}` {
		t.Fatalf("event Worker ref must remain minimal, got %s", raw)
	}
}

func TestEnvelopeV2HasRequiredFieldsAndNoHierarchyFields(t *testing.T) {
	raw, err := json.Marshal(EnvelopeV2{})
	if err != nil {
		t.Fatalf("marshal empty EnvelopeV2: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode EnvelopeV2 fields: %v", err)
	}
	for _, field := range []string{
		"protocol_version", "event_id", "run_id", "session_id", "assignment_id", "worker",
		"run_seq", "worker_seq", "type", "payload", "created_at",
	} {
		if _, ok := fields[field]; !ok {
			t.Errorf("required field %q is missing from JSON", field)
		}
	}
	for _, field := range []string{"root_run_id", "parent_run_id", "agent", "subagent_id"} {
		if _, ok := fields[field]; ok {
			t.Errorf("legacy hierarchy field %q must not appear in EnvelopeV2", field)
		}
	}
}

func TestProtocolVersionV2(t *testing.T) {
	if ProtocolVersionV2 != "2026-07-13" {
		t.Fatalf("ProtocolVersionV2 = %q", ProtocolVersionV2)
	}
}
