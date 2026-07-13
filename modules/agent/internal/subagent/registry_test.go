package subagent

import (
	"sync/atomic"
	"testing"

	"redpanda/protocol/methods"
)

func TestRegistryTerminalStateIsSticky(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Registration{
		SubAgentID: "s1",
		Name:       "worker",
		RootRunID:  "r1",
	})
	if !registry.Finish("r1", "s1", "completed", "done", "") {
		t.Fatal("expected first finish to succeed")
	}
	if registry.Finish("r1", "s1", "failed", "late failure", "boom") {
		t.Fatal("terminal state must not be overwritten")
	}
	record, ok := registry.Lookup("r1", "s1")
	if !ok || record.Status != "completed" || record.Summary != "done" {
		t.Fatalf("record = %#v, ok=%v", record, ok)
	}
}

func TestRegistryCancelRunsOnceAndHonorsRootRun(t *testing.T) {
	registry := NewRegistry()
	var calls atomic.Int32
	registry.Register(Registration{
		SubAgentID: "s1",
		RootRunID:  "r1",
		Cancel:     func() { calls.Add(1) },
	})
	if registry.Cancel("other", "s1") {
		t.Fatal("wrong root run cancelled subagent")
	}
	if !registry.Cancel("r1", "s1") {
		t.Fatal("expected first cancel to succeed")
	}
	if registry.Cancel("r1", "s1") {
		t.Fatal("second cancel must be a no-op")
	}
	if calls.Load() != 1 {
		t.Fatalf("cancel calls = %d", calls.Load())
	}
}

func TestRegistryForceFinishRemoveAndListCopies(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Registration{SubAgentID: "s1", Name: "worker", RootRunID: "r1"})
	registry.Register(Registration{SubAgentID: "s2", Name: "other", RootRunID: "r2"})

	if !registry.ForceFinish("r1", "s1", "reset", "reset by parent", "") {
		t.Fatal("expected force finish to succeed")
	}
	items := registry.List(methods.SubAgentsParams{RunID: "r1"})
	if len(items) != 1 || items[0].Status != "reset" {
		t.Fatalf("items = %#v", items)
	}
	items[0].Status = "mutated"
	record, ok := registry.Lookup("r1", "s1")
	if !ok || record.Status != "reset" {
		t.Fatalf("registry exposed mutable record: %#v", record)
	}
	if !registry.Remove("r1", "s1") {
		t.Fatal("expected remove to succeed")
	}
	if _, ok := registry.Lookup("r1", "s1"); ok {
		t.Fatal("removed record is still present")
	}
}
