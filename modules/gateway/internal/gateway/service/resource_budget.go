package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"redpanda/protocol/methods"
)

const (
	defaultMaxWorkers          = 6
	maxWorkersCap              = 64
	defaultMaxProviderRequests = 4
	maxProviderRequestsCap     = 32
)

type workerBudgetWaiter struct {
	params methods.WorkerPermitParams
	result chan error
	start  time.Time
}

type ResourceBudget struct {
	mu      sync.Mutex
	limit   int
	active  map[string]string
	waiters []*workerBudgetWaiter
}

func NewResourceBudget() *ResourceBudget {
	return newResourceBudget(resolveMaxWorkers())
}

func NewProviderBudget() *ResourceBudget {
	return newResourceBudget(resolveBoundedEnv("RED_PANDA_MAX_PROVIDER_REQUESTS", defaultMaxProviderRequests, maxProviderRequestsCap))
}

func newResourceBudget(limit int) *ResourceBudget {
	return &ResourceBudget{
		limit:  limit,
		active: map[string]string{},
	}
}

func resolveMaxWorkers() int {
	return resolveBoundedEnv("RED_PANDA_MAX_WORKERS", defaultMaxWorkers, maxWorkersCap)
}

func resolveBoundedEnv(name string, fallback int, capValue int) int {
	value := strings.TrimSpace(os.Getenv(name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > capValue {
		return capValue
	}
	return parsed
}

func (b *ResourceBudget) Acquire(ctx context.Context, params methods.WorkerPermitParams) (methods.WorkerPermitResult, error) {
	if b == nil {
		return methods.WorkerPermitResult{Granted: true}, nil
	}
	if params.PermitID == "" || params.RootRunID == "" || params.WorkerID == "" {
		return methods.WorkerPermitResult{}, fmt.Errorf("permit_id, root_run_id, and worker_id are required")
	}
	waiter := &workerBudgetWaiter{params: params, result: make(chan error, 1), start: time.Now()}
	b.mu.Lock()
	if _, exists := b.active[params.PermitID]; exists {
		b.mu.Unlock()
		return methods.WorkerPermitResult{Granted: true}, nil
	}
	b.waiters = append(b.waiters, waiter)
	b.dispatchLocked()
	b.mu.Unlock()

	select {
	case err := <-waiter.result:
		if err != nil {
			return methods.WorkerPermitResult{}, err
		}
		return methods.WorkerPermitResult{Granted: true, WaitMS: time.Since(waiter.start).Milliseconds()}, nil
	case <-ctx.Done():
		b.mu.Lock()
		if b.removeWaiterLocked(waiter) {
			b.mu.Unlock()
			return methods.WorkerPermitResult{}, ctx.Err()
		}
		// A concurrent grant won the race. Release it immediately.
		delete(b.active, params.PermitID)
		b.dispatchLocked()
		b.mu.Unlock()
		return methods.WorkerPermitResult{}, ctx.Err()
	}
}

func (b *ResourceBudget) Release(params methods.WorkerPermitParams) methods.WorkerPermitResult {
	if b == nil || params.PermitID == "" {
		return methods.WorkerPermitResult{Granted: false}
	}
	b.mu.Lock()
	_, existed := b.active[params.PermitID]
	delete(b.active, params.PermitID)
	b.dispatchLocked()
	b.mu.Unlock()
	return methods.WorkerPermitResult{Granted: existed}
}

func (b *ResourceBudget) ReleaseRun(rootRunID string) {
	if b == nil || rootRunID == "" {
		return
	}
	b.mu.Lock()
	for permitID, owner := range b.active {
		if owner == rootRunID {
			delete(b.active, permitID)
		}
	}
	for index := len(b.waiters) - 1; index >= 0; index-- {
		if b.waiters[index].params.RootRunID == rootRunID {
			b.waiters[index].result <- fmt.Errorf("root run ended before worker permit was granted")
			b.waiters = append(b.waiters[:index], b.waiters[index+1:]...)
		}
	}
	b.dispatchLocked()
	b.mu.Unlock()
}

func (b *ResourceBudget) Status() map[string]any {
	if b == nil {
		return map[string]any{"limit": 0, "active": 0, "queued": 0}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return map[string]any{
		"limit":  b.limit,
		"active": len(b.active),
		"queued": len(b.waiters),
	}
}

func (b *ResourceBudget) dispatchLocked() {
	for len(b.waiters) > 0 && len(b.active) < b.limit {
		waiter := b.waiters[0]
		b.waiters = b.waiters[1:]
		b.active[waiter.params.PermitID] = waiter.params.RootRunID
		waiter.result <- nil
	}
}

func (b *ResourceBudget) removeWaiterLocked(target *workerBudgetWaiter) bool {
	for index, waiter := range b.waiters {
		if waiter != target {
			continue
		}
		b.waiters = append(b.waiters[:index], b.waiters[index+1:]...)
		return true
	}
	return false
}
