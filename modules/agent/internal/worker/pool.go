package worker

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Pool struct {
	mu              sync.Mutex
	executorMu      sync.RWMutex
	config          Config
	workers         []*Worker
	workerByID      map[WorkerID]*Worker
	assignments     map[AssignmentID]*Assignment
	runAssignments  map[string]map[AssignmentID]struct{}
	assignmentOrder []AssignmentID
	nextWorker      int
	assignmentSeq   atomic.Uint64
	messageSeq      atomic.Uint64
	closed          bool
	wg              sync.WaitGroup
	cancelWG        sync.WaitGroup
	closeDone       chan struct{}
	closeErr        error
}

func NewPool(config Config, factory ExecutorFactory) (*Pool, error) {
	config = normalizeConfig(config)
	if config.Size < 1 || config.Size > MaxPoolSize || config.MailboxCapacity < 1 || config.MaxMessageBytes < 1 || config.MessageTTL < 1 || factory == nil {
		return nil, ErrInvalidConfig
	}
	pool := &Pool{
		config:         config,
		workerByID:     make(map[WorkerID]*Worker, config.Size),
		assignments:    make(map[AssignmentID]*Assignment),
		runAssignments: make(map[string]map[AssignmentID]struct{}),
		closeDone:      make(chan struct{}),
	}
	for i := 0; i < config.Size; i++ {
		id := WorkerID(fmt.Sprintf("worker-%02d", i+1))
		executor, err := factory(id)
		if err != nil || executor == nil {
			pool.closeConstructed(context.Background())
			if err == nil {
				err = errors.New("executor factory returned nil")
			}
			return nil, fmt.Errorf("create %s executor: %w", id, err)
		}
		mailbox, err := NewMailbox(config.MailboxCapacity)
		if err != nil {
			_ = executor.Close(context.Background())
			pool.closeConstructed(context.Background())
			return nil, fmt.Errorf("create %s mailbox: %w", id, err)
		}
		state := WorkerReady
		if !executor.Healthy() {
			state = WorkerUnhealthy
		}
		worker := &Worker{ID: id, state: state, mailbox: mailbox, executor: executor}
		pool.workers = append(pool.workers, worker)
		pool.workerByID[id] = worker
	}
	return pool, nil
}

func normalizeConfig(config Config) Config {
	if config.Size == 0 {
		config.Size = DefaultPoolSize
	}
	if config.MailboxCapacity == 0 {
		config.MailboxCapacity = DefaultMailboxCapacity
	}
	if config.MaxMessageBytes == 0 {
		config.MaxMessageBytes = DefaultMaxMessageBytes
	}
	if config.MessageTTL == 0 {
		config.MessageTTL = DefaultMessageTTL
	}
	return config
}

// SubmitEntry creates the trusted entry assignment for a run. Runtime callers
// must use Delegate for requests originating from a Worker assignment.
// ctx is the assignment lifetime context; Runtime should pass the run context,
// not a short-lived transport or tool-call context.
func (p *Pool) SubmitEntry(ctx context.Context, request SubmitRequest) (AssignmentRef, error) {
	return p.submit(ctx, request, "", "")
}

// SubmitRestricted creates a trusted entry-shaped assignment that cannot
// delegate. It is used when a delegated assignment crosses a Runtime process
// boundary and the local Pool must preserve the remote origin restriction.
func (p *Pool) SubmitRestricted(ctx context.Context, originWorkerID WorkerID, request SubmitRequest) (AssignmentRef, error) {
	if originWorkerID == "" {
		return AssignmentRef{}, ErrInvalidConfig
	}
	return p.submit(ctx, request, "", originWorkerID)
}

// Delegate creates one assignment on behalf of an active entry assignment.
func (p *Pool) Delegate(ctx context.Context, callerID AssignmentID, request SubmitRequest) (AssignmentRef, error) {
	if callerID == "" {
		return AssignmentRef{}, ErrAssignmentNotFound
	}
	return p.submit(ctx, request, callerID, "")
}

func (p *Pool) submit(ctx context.Context, request SubmitRequest, callerID AssignmentID, restrictedOrigin WorkerID) (AssignmentRef, error) {
	if err := ctx.Err(); err != nil {
		return AssignmentRef{}, err
	}
	if request.RunID == "" {
		return AssignmentRef{}, fmt.Errorf("%w: run id is required", ErrInvalidConfig)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return AssignmentRef{}, ErrPoolClosed
	}

	origin := restrictedOrigin
	if callerID != "" {
		caller := p.assignments[callerID]
		if caller == nil {
			return AssignmentRef{}, ErrAssignmentNotFound
		}
		if caller.RunID != request.RunID {
			return AssignmentRef{}, ErrCallerRunMismatch
		}
		if caller.OriginWorkerID != "" {
			return AssignmentRef{}, ErrNestedDelegation
		}
		if caller.Status != AssignmentRunning || p.workerByID[caller.WorkerID].currentAssignmentID != caller.ID {
			return AssignmentRef{}, ErrAssignmentNotActive
		}
		origin = caller.WorkerID
	}

	worker := p.nextReadyWorkerLocked()
	if worker == nil {
		return AssignmentRef{}, ErrCapacityExhausted
	}
	now := time.Now().UTC()
	id := AssignmentID(fmt.Sprintf("assignment-%06d", p.assignmentSeq.Add(1)))
	execCtx, cancel := context.WithCancel(ctx)
	assignment := &Assignment{
		ID:             id,
		RunID:          request.RunID,
		WorkerID:       worker.ID,
		OriginWorkerID: origin,
		ProfileKey:     request.ProfileKey,
		Task:           request.Task,
		Attempt:        maxInt(request.Attempt, 1),
		RetryOf:        request.RetryOf,
		Status:         AssignmentQueued,
		CreatedAt:      now,
		cancel:         cancel,
		done:           make(chan struct{}),
		settled:        make(chan struct{}),
	}
	p.assignments[id] = assignment
	p.assignmentOrder = append(p.assignmentOrder, id)
	if p.runAssignments[request.RunID] == nil {
		p.runAssignments[request.RunID] = make(map[AssignmentID]struct{})
	}
	p.runAssignments[request.RunID][id] = struct{}{}
	worker.state = WorkerBusy
	worker.currentAssignmentID = id
	p.wg.Add(1)
	go p.execute(execCtx, worker, assignment, request.EventSink)
	return AssignmentRef{AssignmentID: id, WorkerID: worker.ID, OriginWorkerID: origin,
		Attempt: assignment.Attempt, RetryOf: assignment.RetryOf}, nil
}

func (p *Pool) nextReadyWorkerLocked() *Worker {
	for offset := 0; offset < len(p.workers); offset++ {
		index := (p.nextWorker + offset) % len(p.workers)
		if p.workers[index].state == WorkerReady {
			p.nextWorker = (index + 1) % len(p.workers)
			return p.workers[index]
		}
	}
	return nil
}

func (p *Pool) execute(ctx context.Context, worker *Worker, assignment *Assignment, sink EventSink) {
	defer p.wg.Done()
	now := time.Now().UTC()
	p.mu.Lock()
	cancel := assignment.cancel
	active := !assignment.Status.Terminal()
	if active {
		assignment.Status = AssignmentRunning
		assignment.StartedAt = &now
	}
	p.mu.Unlock()

	var result ExecuteResult
	var err error
	if active {
		result, err = worker.executor.Execute(ctx, ExecuteRequest{
			AssignmentID: assignment.ID,
			RunID:        assignment.RunID,
			WorkerID:     assignment.WorkerID,
			ProfileKey:   assignment.ProfileKey,
			Task:         assignment.Task,
		}, sink)
	}
	if cancel != nil {
		cancel()
	}
	p.mu.Lock()
	assignment.cancel = nil
	assignment.Stats = result.Stats
	if !assignment.Status.Terminal() {
		finished := time.Now().UTC()
		assignment.FinishedAt = &finished
		if err != nil {
			assignment.Status = AssignmentFailed
			assignment.Error = err.Error()
		} else {
			assignment.Status = AssignmentCompleted
			assignment.Result = result.Output
		}
		close(assignment.done)
	}
	poolOpen := !p.closed
	cancelDone := assignment.cancelDone
	p.mu.Unlock()
	if cancelDone != nil {
		<-cancelDone
	}
	healthy := false
	if poolOpen {
		healthy = worker.executor.Healthy()
		if active && (err != nil || !healthy) {
			resetErr := worker.executor.Reset(context.Background())
			healthy = resetErr == nil && worker.executor.Healthy()
		}
	}

	p.mu.Lock()
	if worker.currentAssignmentID == assignment.ID {
		worker.currentAssignmentID = ""
		if p.closed {
			worker.state = WorkerStopped
		} else if healthy {
			worker.state = WorkerReady
		} else {
			worker.state = WorkerUnhealthy
		}
	}
	close(assignment.settled)
	p.mu.Unlock()
}

func (p *Pool) Wait(ctx context.Context, id AssignmentID) (AssignmentResult, error) {
	p.mu.Lock()
	assignment := p.assignments[id]
	if assignment == nil {
		p.mu.Unlock()
		return AssignmentResult{}, ErrAssignmentNotFound
	}
	done := assignment.done
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return AssignmentResult{}, ctx.Err()
	case <-done:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return assignmentResult(assignment), nil
}

// WaitSettled waits until Execute has returned and the Worker slot has been
// released. A cancelled Assignment can be terminal before it is settled.
func (p *Pool) WaitSettled(ctx context.Context, id AssignmentID) (AssignmentResult, error) {
	p.mu.Lock()
	assignment := p.assignments[id]
	if assignment == nil {
		p.mu.Unlock()
		return AssignmentResult{}, ErrAssignmentNotFound
	}
	settled := assignment.settled
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return AssignmentResult{}, ctx.Err()
	case <-settled:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return assignmentResult(assignment), nil
}

// SetWaitingPermission projects permission suspension into the assignment
// ledger without changing Worker slot ownership.
func (p *Pool) SetWaitingPermission(id AssignmentID, waiting bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	assignment := p.assignments[id]
	if assignment == nil {
		return ErrAssignmentNotFound
	}
	if assignment.Status.Terminal() {
		return ErrAssignmentNotActive
	}
	if waiting {
		if assignment.Status != AssignmentRunning {
			return ErrAssignmentNotActive
		}
		assignment.Status = AssignmentWaitingPermission
		return nil
	}
	if assignment.Status != AssignmentWaitingPermission {
		return ErrAssignmentNotActive
	}
	assignment.Status = AssignmentRunning
	return nil
}

func (p *Pool) Cancel(ctx context.Context, id AssignmentID, reason string) bool {
	_ = ctx // Cancellation is owned by the assignment lifecycle, not caller wait time.
	p.mu.Lock()
	assignment := p.assignments[id]
	if assignment == nil || assignment.Status.Terminal() {
		p.mu.Unlock()
		return false
	}
	finished := time.Now().UTC()
	assignment.Status = AssignmentCancelled
	assignment.Error = reason
	assignment.FinishedAt = &finished
	assignment.cancel()
	assignment.cancel = nil
	close(assignment.done)
	worker := p.workerByID[assignment.WorkerID]
	if worker != nil {
		assignment.cancelDone = make(chan struct{})
		p.cancelWG.Add(1)
	}
	p.mu.Unlock()
	if worker != nil {
		go func() {
			defer p.cancelWG.Done()
			_ = worker.executor.Cancel(context.Background(), id, reason)
			close(assignment.cancelDone)
		}()
	}
	return true
}

// CancelRun cancels every non-terminal assignment belonging to runID.
func (p *Pool) CancelRun(ctx context.Context, runID, reason string) int {
	p.mu.Lock()
	ids := make([]AssignmentID, 0, len(p.runAssignments[runID]))
	for id := range p.runAssignments[runID] {
		ids = append(ids, id)
	}
	p.mu.Unlock()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	cancelled := 0
	for _, id := range ids {
		if p.Cancel(ctx, id, reason) {
			cancelled++
		}
	}
	return cancelled
}

// PauseDelegatedRun cooperatively suspends delegated assignments while leaving
// the entry assignment (the main Worker) active.
func (p *Pool) PauseDelegatedRun(ctx context.Context, runID, reason string) (int, error) {
	type target struct {
		id       AssignmentID
		executor PausableExecutor
	}
	p.mu.Lock()
	targets := make([]target, 0, len(p.runAssignments[runID]))
	for id := range p.runAssignments[runID] {
		assignment := p.assignments[id]
		if assignment == nil || assignment.OriginWorkerID == "" || assignment.Status.Terminal() || assignment.Status == AssignmentPaused {
			continue
		}
		worker := p.workerByID[assignment.WorkerID]
		pausable, ok := worker.executor.(PausableExecutor)
		if !ok {
			p.mu.Unlock()
			return 0, fmt.Errorf("%s executor does not support pause", worker.ID)
		}
		targets = append(targets, target{id: id, executor: pausable})
	}
	p.mu.Unlock()
	sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })
	paused := 0
	var pauseErr error
	for _, item := range targets {
		if err := item.executor.Pause(ctx, item.id, reason); err != nil {
			pauseErr = errors.Join(pauseErr, err)
			continue
		}
		p.mu.Lock()
		assignment := p.assignments[item.id]
		if assignment != nil && !assignment.Status.Terminal() && assignment.Status != AssignmentPaused {
			assignment.resumeStatus = assignment.Status
			assignment.Status = AssignmentPaused
			paused++
		}
		p.mu.Unlock()
	}
	return paused, pauseErr
}

func (p *Pool) ResumeDelegatedRun(ctx context.Context, runID string) (int, error) {
	type target struct {
		id       AssignmentID
		executor PausableExecutor
	}
	p.mu.Lock()
	var targets []target
	for id := range p.runAssignments[runID] {
		assignment := p.assignments[id]
		if assignment == nil || assignment.Status != AssignmentPaused {
			continue
		}
		worker := p.workerByID[assignment.WorkerID]
		pausable, ok := worker.executor.(PausableExecutor)
		if !ok {
			p.mu.Unlock()
			return 0, fmt.Errorf("%s executor does not support resume", worker.ID)
		}
		targets = append(targets, target{id: id, executor: pausable})
	}
	p.mu.Unlock()
	sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })
	resumed := 0
	var resumeErr error
	for _, item := range targets {
		if err := item.executor.Resume(ctx, item.id); err != nil {
			resumeErr = errors.Join(resumeErr, err)
			continue
		}
		p.mu.Lock()
		assignment := p.assignments[item.id]
		if assignment != nil && assignment.Status == AssignmentPaused {
			assignment.Status = assignment.resumeStatus
			if assignment.Status == "" || assignment.Status == AssignmentPaused {
				assignment.Status = AssignmentRunning
			}
			assignment.resumeStatus = ""
			resumed++
		}
		p.mu.Unlock()
	}
	return resumed, resumeErr
}

func (p *Pool) Send(ctx context.Context, message WorkerMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if message.RunID == "" || message.FromWorkerID == "" || message.ToWorkerID == "" || !validMessageKind(message.Kind) {
		return ErrInvalidMessage
	}
	if len(message.Payload) > p.config.MaxMessageBytes {
		return ErrMessageTooLarge
	}
	if !message.ExpiresAt.IsZero() && !message.ExpiresAt.After(now) {
		return ErrMessageExpired
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrPoolClosed
	}
	from := p.workerByID[message.FromWorkerID]
	to := p.workerByID[message.ToWorkerID]
	if from == nil || to == nil {
		p.mu.Unlock()
		return ErrWorkerNotFound
	}
	fromAssignment := p.currentAssignmentLocked(from)
	toAssignment := p.currentAssignmentLocked(to)
	if fromAssignment == nil || toAssignment == nil || fromAssignment.RunID != message.RunID || toAssignment.RunID != message.RunID {
		p.mu.Unlock()
		return ErrCrossRunMessage
	}
	if message.ID == "" {
		message.ID = fmt.Sprintf("message-%06d", p.messageSeq.Add(1))
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = now
	}
	if message.ExpiresAt.IsZero() {
		message.ExpiresAt = now.Add(p.config.MessageTTL)
	}
	message.FromAssignmentID = fromAssignment.ID
	message.ToAssignmentID = toAssignment.ID
	mailbox := to.mailbox
	err := mailbox.Deliver(message)
	p.mu.Unlock()
	return err
}

func validMessageKind(kind MessageKind) bool {
	return kind == MessageRequest || kind == MessageUpdate || kind == MessageResult || kind == MessageControl
}

func (p *Pool) currentAssignmentLocked(worker *Worker) *Assignment {
	assignment := p.assignments[worker.currentAssignmentID]
	if assignment == nil || assignment.Status.Terminal() {
		return nil
	}
	return assignment
}

func (p *Pool) Receive(ctx context.Context, assignmentID AssignmentID) (WorkerMessage, error) {
	p.mu.Lock()
	assignment := p.assignments[assignmentID]
	if assignment == nil {
		p.mu.Unlock()
		return WorkerMessage{}, ErrAssignmentNotFound
	}
	worker := p.workerByID[assignment.WorkerID]
	if assignment.Status.Terminal() || worker == nil || worker.currentAssignmentID != assignment.ID {
		p.mu.Unlock()
		return WorkerMessage{}, ErrAssignmentNotActive
	}
	runID := assignment.RunID
	done := assignment.done
	p.mu.Unlock()
	message, err := worker.mailbox.Receive(ctx, runID, assignmentID, done)
	if err != nil {
		return WorkerMessage{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if assignment.Status.Terminal() || worker.currentAssignmentID != assignment.ID {
		return WorkerMessage{}, ErrAssignmentNotActive
	}
	return message, nil
}

func (p *Pool) Snapshot() PoolSnapshot {
	p.executorMu.RLock()
	defer p.executorMu.RUnlock()
	p.mu.Lock()
	snapshot := PoolSnapshot{Configured: len(p.workers)}
	executors := make([]Executor, 0, len(p.workers))
	for _, worker := range p.workers {
		workerSnapshot := WorkerSnapshot{
			ID:                  worker.ID,
			State:               worker.state,
			CurrentAssignmentID: worker.currentAssignmentID,
			MailboxDepth:        worker.mailbox.Len(),
			MailboxCapacity:     worker.mailbox.Cap(),
		}
		executors = append(executors, worker.executor)
		if assignment := p.assignments[worker.currentAssignmentID]; assignment != nil {
			workerSnapshot.ProfileKey = assignment.ProfileKey
		}
		snapshot.Workers = append(snapshot.Workers, workerSnapshot)
		switch worker.state {
		case WorkerReady:
			snapshot.Ready++
		case WorkerBusy:
			snapshot.Busy++
		case WorkerDraining:
			snapshot.Draining++
		case WorkerUnhealthy:
			snapshot.Unhealthy++
		case WorkerStopped:
			snapshot.Stopped++
		}
	}
	for _, id := range p.assignmentOrder {
		assignment := p.assignments[id]
		snapshot.Assignments = append(snapshot.Assignments, snapshotAssignment(assignment))
		switch assignment.Status {
		case AssignmentQueued:
			snapshot.Queued++
		case AssignmentRunning:
			snapshot.Running++
		case AssignmentWaitingPermission:
			snapshot.WaitingPermission++
		case AssignmentPaused:
			snapshot.Paused++
		}
	}
	p.mu.Unlock()
	for i, executor := range executors {
		snapshot.Workers[i].Healthy = executor.Healthy()
	}
	return snapshot
}

func snapshotAssignment(assignment *Assignment) AssignmentSnapshot {
	return AssignmentSnapshot{
		ID:             assignment.ID,
		RunID:          assignment.RunID,
		WorkerID:       assignment.WorkerID,
		OriginWorkerID: assignment.OriginWorkerID,
		ProfileKey:     assignment.ProfileKey,
		Task:           assignment.Task,
		Attempt:        assignment.Attempt,
		RetryOf:        assignment.RetryOf,
		Status:         assignment.Status,
		Result:         assignment.Result,
		Error:          assignment.Error,
		Stats:          assignment.Stats,
		CreatedAt:      assignment.CreatedAt,
		StartedAt:      assignment.StartedAt,
		FinishedAt:     assignment.FinishedAt,
	}
}

func assignmentResult(assignment *Assignment) AssignmentResult {
	return AssignmentResult{
		AssignmentID: assignment.ID,
		WorkerID:     assignment.WorkerID,
		Attempt:      assignment.Attempt,
		RetryOf:      assignment.RetryOf,
		Status:       assignment.Status,
		Output:       assignment.Result,
		Error:        assignment.Error,
		Stats:        assignment.Stats,
	}
}

func maxInt(value, fallback int) int {
	if value > fallback {
		return value
	}
	return fallback
}

func (p *Pool) Close(ctx context.Context) error {
	p.mu.Lock()
	first := !p.closed
	if first {
		p.closed = true
		var active []AssignmentID
		for id, assignment := range p.assignments {
			if !assignment.Status.Terminal() {
				active = append(active, id)
			}
		}
		sort.Slice(active, func(i, j int) bool { return active[i] < active[j] })
		p.mu.Unlock()
		for _, id := range active {
			p.Cancel(context.Background(), id, "worker pool closed")
		}
		go p.finishClose()
	} else {
		p.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closeDone:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.closeErr
	}
}

func (p *Pool) finishClose() {
	p.wg.Wait()
	p.cancelWG.Wait()
	p.executorMu.Lock()
	defer p.executorMu.Unlock()

	var closeErr error
	p.mu.Lock()
	workers := append([]*Worker(nil), p.workers...)
	p.mu.Unlock()
	for _, worker := range workers {
		if err := worker.executor.Close(context.Background()); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close %s executor: %w", worker.ID, err))
		}
		worker.mailbox.Close()
		p.mu.Lock()
		worker.state = WorkerStopped
		worker.currentAssignmentID = ""
		p.mu.Unlock()
	}
	p.mu.Lock()
	p.closeErr = closeErr
	close(p.closeDone)
	p.mu.Unlock()
}

// RemoveTerminal releases retained task/result/context metadata for a terminal
// assignment. Active assignments are never removed.
func (p *Pool) RemoveTerminal(id AssignmentID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	assignment := p.assignments[id]
	if assignment == nil || !assignment.Status.Terminal() {
		return false
	}
	if worker := p.workerByID[assignment.WorkerID]; worker != nil && worker.currentAssignmentID == id {
		return false
	}
	delete(p.assignments, id)
	if run := p.runAssignments[assignment.RunID]; run != nil {
		delete(run, id)
		if len(run) == 0 {
			delete(p.runAssignments, assignment.RunID)
		}
	}
	for i, assignmentID := range p.assignmentOrder {
		if assignmentID == id {
			p.assignmentOrder = append(p.assignmentOrder[:i], p.assignmentOrder[i+1:]...)
			break
		}
	}
	return true
}

func (p *Pool) closeConstructed(ctx context.Context) {
	for _, worker := range p.workers {
		_ = worker.executor.Close(ctx)
		worker.mailbox.Close()
	}
}
