package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/methods"
)

func TestResourceBudgetQueuesFIFOAndReleasesGlobally(t *testing.T) {
	budget := &ResourceBudget{limit: 1, active: map[string]string{}}
	first := methods.WorkerPermitParams{PermitID: "p1", RootRunID: "r1", WorkerID: "w1"}
	if result, err := budget.Acquire(context.Background(), first); err != nil || !result.Granted {
		t.Fatalf("first acquire = %#v, %v", result, err)
	}

	results := make(chan string, 2)
	acquire := func(params methods.WorkerPermitParams) {
		_, err := budget.Acquire(context.Background(), params)
		if err != nil {
			results <- params.PermitID + ":" + err.Error()
			return
		}
		results <- params.PermitID
	}
	go acquire(methods.WorkerPermitParams{PermitID: "p2", RootRunID: "r2", WorkerID: "w2"})
	waitForBudgetQueued(t, budget, 1)
	go acquire(methods.WorkerPermitParams{PermitID: "p3", RootRunID: "r3", WorkerID: "w3"})
	waitForBudgetQueued(t, budget, 2)

	budget.Release(first)
	if got := <-results; got != "p2" {
		t.Fatalf("first queued permit = %q, want p2", got)
	}
	budget.Release(methods.WorkerPermitParams{PermitID: "p2"})
	if got := <-results; got != "p3" {
		t.Fatalf("second queued permit = %q, want p3", got)
	}
}

func TestResourceBudgetReleaseRunWakesQueuedPermit(t *testing.T) {
	budget := &ResourceBudget{limit: 1, active: map[string]string{}}
	if _, err := budget.Acquire(context.Background(), methods.WorkerPermitParams{
		PermitID: "holder", RootRunID: "holder-run", WorkerID: "holder-worker",
	}); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := budget.Acquire(context.Background(), methods.WorkerPermitParams{
			PermitID: "queued", RootRunID: "queued-run", WorkerID: "queued-worker",
		})
		result <- err
	}()
	waitForBudgetQueued(t, budget, 1)
	budget.ReleaseRun("queued-run")
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "root run ended") {
			t.Fatalf("queued release error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued permit was not woken when root run ended")
	}
}

func waitForBudgetQueued(t *testing.T, budget *ResourceBudget, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if budget.Status()["queued"] == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queued = %v, want %d", budget.Status()["queued"], want)
}
