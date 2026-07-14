package methods

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestWorkerMethodNames(t *testing.T) {
	want := map[string]string{
		"run execute":              "run.execute",
		"run cancel":               "run.cancel",
		"run event":                "run.event",
		"worker list":              "worker.list",
		"worker assignment cancel": "worker.assignment.cancel",
		"worker message send":      "worker.message.send",
		"worker message receive":   "worker.message.receive",
		"worker pool status":       "worker.pool.status",
	}
	got := map[string]string{
		"run execute":              RunExecute,
		"run cancel":               RunCancel,
		"run event":                RunEvent,
		"worker list":              WorkerList,
		"worker assignment cancel": WorkerAssignmentCancel,
		"worker message send":      WorkerMessageSend,
		"worker message receive":   WorkerMessageReceive,
		"worker pool status":       WorkerPoolStatus,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("method constants mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWorkerContractJSONRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(time.Second)
	expiresAt := createdAt.Add(5 * time.Minute)
	want := struct {
		Pool    PoolSnapshot  `json:"pool"`
		Message WorkerMessage `json:"message"`
	}{
		Pool: PoolSnapshot{
			Configured: 8,
			Ready:      1,
			Busy:       1,
			Running:    1,
			Workers: []WorkerRef{
				{ID: "worker-01", State: WorkerStateBusy, CurrentAssignmentID: "assignment-01", ProfileKey: "planner", MailboxCapacity: 64, Healthy: true},
				{ID: "worker-02", State: WorkerStateReady, MailboxCapacity: 64, Healthy: true},
			},
			Assignments: []AssignmentRecord{{
				ID: "assignment-01", RunID: "run-01", WorkerID: "worker-01",
				ProfileKey: "planner", Task: "plan the work", Status: AssignmentStatusRunning,
				CreatedAt: createdAt, StartedAt: &startedAt,
			}},
		},
		Message: WorkerMessage{
			ID: "message-01", RunID: "run-01", FromWorkerID: "worker-01", FromAssignmentID: "assignment-01",
			ToWorkerID: "worker-02", ToAssignmentID: "assignment-02",
			Kind: MessageKindRequest, CorrelationID: "question-01", Payload: json.RawMessage(`{"question":"review this"}`),
			CreatedAt: createdAt, ExpiresAt: expiresAt,
		},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal worker contract: %v", err)
	}
	var got typeofWorkerContract
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal worker contract: %v", err)
	}
	if !reflect.DeepEqual(got.Pool, want.Pool) || !reflect.DeepEqual(got.Message, want.Message) {
		t.Fatalf("round trip mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

type typeofWorkerContract struct {
	Pool    PoolSnapshot  `json:"pool"`
	Message WorkerMessage `json:"message"`
}

func TestRequiredWorkerFieldsAreNotOmitted(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		required []string
	}{
		{"worker ref", WorkerRef{}, []string{"id", "healthy"}},
		{"assignment", AssignmentRecord{}, []string{"id", "run_id", "worker_id", "task", "status", "created_at"}},
		{"message", WorkerMessage{}, []string{"id", "run_id", "from_worker_id", "from_assignment_id", "to_worker_id", "to_assignment_id", "kind", "payload", "created_at", "expires_at"}},
		{"message send", WorkerMessageSendParams{}, []string{"to_worker_id", "to_assignment_id", "kind", "payload"}},
		{"pool", PoolSnapshot{}, []string{"configured", "ready", "busy", "draining", "unhealthy", "stopped", "queued", "running", "waiting_permission", "workers", "assignments"}},
		{"assignment cancel", WorkerAssignmentCancelParams{}, []string{"run_id", "assignment_id"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatalf("decode fields: %v", err)
			}
			for _, field := range tc.required {
				if _, ok := fields[field]; !ok {
					t.Errorf("required field %q is missing from JSON", field)
				}
			}
		})
	}
}

func TestWorkerMessageSendParamsExcludeServerOwnedFields(t *testing.T) {
	raw, err := json.Marshal(WorkerMessageSendParams{
		ToWorkerID:     "worker-02",
		ToAssignmentID: "assignment-02",
		Kind:           MessageKindRequest,
		CorrelationID:  "question-01",
		ReplyTo:        "message-00",
		Payload:        json.RawMessage(`{"question":"review this"}`),
	})
	if err != nil {
		t.Fatalf("marshal message send params: %v", err)
	}
	for _, forbidden := range []string{
		"id", "run_id", "from_worker_id", "from_assignment_id", "created_at", "expires_at",
	} {
		if containsJSONField(string(raw), forbidden) {
			t.Errorf("message send params must not contain server-owned field %q: %s", forbidden, raw)
		}
	}
}

func TestWorkerMessageReceiveParamsExcludeCallerIdentity(t *testing.T) {
	raw, err := json.Marshal(WorkerMessageReceiveParams{TimeoutMS: 5000})
	if err != nil {
		t.Fatalf("marshal message receive params: %v", err)
	}
	if string(raw) != `{"timeout_ms":5000}` {
		t.Fatalf("receive params must contain only timeout_ms, got %s", raw)
	}
	for _, forbidden := range []string{
		"run_id", "worker_id", "assignment_id", "from_worker_id", "from_assignment_id",
	} {
		if containsJSONField(string(raw), forbidden) {
			t.Errorf("message receive params must not contain caller identity field %q: %s", forbidden, raw)
		}
	}
}

func TestWorkerExecutionContextRoundTrip(t *testing.T) {
	context := &WorkerExecutionContext{
		WorkerID:      "worker-02",
		AssignmentID:  "assignment-02",
		RunID:         "run-01",
		ProxyMessages: true,
	}
	v2Raw, err := json.Marshal(RunExecuteOptions{WorkerContext: context})
	if err != nil {
		t.Fatalf("marshal v2 Worker context: %v", err)
	}
	legacyRaw, err := json.Marshal(ReplyOptions{WorkerContext: context})
	if err != nil {
		t.Fatalf("marshal legacy Worker context: %v", err)
	}
	for name, raw := range map[string][]byte{"v2": v2Raw, "legacy": legacyRaw} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("decode %s options: %v", name, err)
		}
		workerRaw, ok := fields["worker_context"]
		if !ok {
			t.Fatalf("%s options omitted worker_context: %s", name, raw)
		}
		var got WorkerExecutionContext
		if err := json.Unmarshal(workerRaw, &got); err != nil {
			t.Fatalf("decode %s Worker context: %v", name, err)
		}
		if !reflect.DeepEqual(got, *context) {
			t.Fatalf("%s Worker context mismatch: got %#v want %#v", name, got, *context)
		}
	}
}

func TestWorkerListResultPreservesEmptyCollections(t *testing.T) {
	raw, err := json.Marshal(WorkerListResult{Workers: []WorkerRef{}, Assignments: []AssignmentRecord{}})
	if err != nil {
		t.Fatalf("marshal list result: %v", err)
	}
	if string(raw) != `{"workers":[],"assignments":[]}` {
		t.Fatalf("empty collections must remain present, got %s", raw)
	}
}

func TestRunExecuteOptionsDoNotExposeLegacySubAgentFields(t *testing.T) {
	raw, err := json.Marshal(RunExecuteParams{
		RunID: "run-01",
		Options: RunExecuteOptions{WorkerProfiles: []WorkerProfileRef{{
			Key: "goal-verifier", Enabled: true,
		}}},
	})
	if err != nil {
		t.Fatalf("marshal run execute params: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode run execute params: %v", err)
	}
	for _, required := range []string{"run_id", "session", "input", "options"} {
		if _, ok := fields[required]; !ok {
			t.Errorf("required field %q is missing", required)
		}
	}
	serialized := string(raw)
	for _, forbidden := range []string{"spawn_subagents", "subagent_backend", "agent_definitions"} {
		if json.Valid(raw) && containsJSONField(serialized, forbidden) {
			t.Errorf("v2 run options must not contain legacy field %q: %s", forbidden, raw)
		}
	}
}

func TestWorkerRPCPayloadsJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		value any
	}{
		{"run execute params", RunExecuteParams{RunID: "run-01", Session: ReplySession{ID: "session-01"}, Input: ReplyInput{Text: "work"}}},
		{"run execute result", RunExecuteResult{Accepted: true, RunID: "run-01", AssignmentID: "assignment-01", WorkerID: "worker-01"}},
		{"run cancel params", RunCancelParams{RunID: "run-01", Reason: "user request"}},
		{"run cancel result", RunCancelResult{Accepted: true, RunID: "run-01", Cancelled: 2}},
		{"worker list params", WorkerListParams{RunID: "run-01", WorkerID: "worker-01", AssignmentID: "assignment-01"}},
		{"worker list result", WorkerListResult{Workers: []WorkerRef{{ID: "worker-01", Healthy: true}}, Assignments: []AssignmentRecord{}}},
		{"assignment cancel params", WorkerAssignmentCancelParams{RunID: "run-01", AssignmentID: "assignment-01", Reason: "not needed"}},
		{"assignment cancel result", WorkerAssignmentCancelResult{Accepted: true, RunID: "run-01", AssignmentID: "assignment-01", Cancelled: true}},
		{"message send params", WorkerMessageSendParams{ToWorkerID: "worker-02", ToAssignmentID: "assignment-02", Kind: MessageKindUpdate, Payload: json.RawMessage(`{"progress":50}`)}},
		{"message send result", WorkerMessageSendResult{Accepted: true, Message: WorkerMessage{ID: "message-01", RunID: "run-01", FromWorkerID: "worker-01", FromAssignmentID: "assignment-01", ToWorkerID: "worker-02", ToAssignmentID: "assignment-02", Kind: MessageKindUpdate, Payload: json.RawMessage(`{"progress":50}`), CreatedAt: now, ExpiresAt: now.Add(time.Minute)}}},
		{"message receive params", WorkerMessageReceiveParams{TimeoutMS: 5000}},
		{"message receive result", WorkerMessageReceiveResult{Found: true, Message: WorkerMessage{ID: "message-01", RunID: "run-01", FromWorkerID: "worker-01", FromAssignmentID: "assignment-01", ToWorkerID: "worker-02", ToAssignmentID: "assignment-02", Kind: MessageKindUpdate, Payload: json.RawMessage(`{"progress":50}`), CreatedAt: now, ExpiresAt: now.Add(time.Minute)}}},
		{"pool status params", WorkerPoolStatusParams{}},
		{"pool status result", WorkerPoolStatusResult{Pool: PoolSnapshot{Configured: 8, Workers: []WorkerRef{}, Assignments: []AssignmentRecord{}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := reflect.New(reflect.TypeOf(tc.value))
			if err := json.Unmarshal(raw, got.Interface()); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got.Elem().Interface(), tc.value) {
				t.Fatalf("round trip mismatch:\n got: %#v\nwant: %#v", got.Elem().Interface(), tc.value)
			}
		})
	}
}

func containsJSONField(raw, field string) bool {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return false
	}
	return containsField(value, field)
}

func containsField(value any, field string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed[field]; ok {
			return true
		}
		for _, child := range typed {
			if containsField(child, field) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsField(child, field) {
				return true
			}
		}
	}
	return false
}
