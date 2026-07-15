package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/agent/internal/worker"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	protocoltools "redpanda/protocol/tools"
)

type workerExecutionKind string

const (
	workerExecutionEntry     workerExecutionKind = "entry"
	workerExecutionDelegated workerExecutionKind = "delegated"
)

type workerExecutionSpec struct {
	Kind       workerExecutionKind
	Params     methods.ReplyParams
	Parent     methods.ReplyParams
	ProfileKey string
	Task       string
	MaxTurns   int
}

type workerExecutionContextKey struct{}
type assignmentContextKey struct{}

type assignmentContext struct {
	AssignmentID worker.AssignmentID
	WorkerID     worker.WorkerID
}

type workerProcessFactory func(
	context.Context,
	methods.ReplyParams,
	string,
	func(context.Context, string, any) (json.RawMessage, error),
) (worker.Process, error)

func withWorkerExecution(ctx context.Context, spec workerExecutionSpec) context.Context {
	return context.WithValue(ctx, workerExecutionContextKey{}, spec)
}

func withAssignment(ctx context.Context, assignmentID worker.AssignmentID, workerID worker.WorkerID) context.Context {
	return context.WithValue(ctx, assignmentContextKey{}, assignmentContext{AssignmentID: assignmentID, WorkerID: workerID})
}

func assignmentFromContext(ctx context.Context) assignmentContext {
	assignment, _ := ctx.Value(assignmentContextKey{}).(assignmentContext)
	return assignment
}

// lazyProcessExecutor is created eagerly with its Worker slot, while the OS
// process is created only for the first delegated Assignment executed there.
type lazyProcessExecutor struct {
	runtime  *Runtime
	workerID worker.WorkerID

	mu                 sync.Mutex
	process            worker.Process
	activeAssignmentID worker.AssignmentID
	activeRunID        string
	activeKind         workerExecutionKind
	processCtx         context.Context
	processCancel      context.CancelFunc
	newProcess         workerProcessFactory
	closed             bool
}

func newLazyProcessExecutor(runtime *Runtime, workerID worker.WorkerID) *lazyProcessExecutor {
	processCtx, processCancel := context.WithCancel(context.Background())
	return &lazyProcessExecutor{
		runtime: runtime, workerID: workerID,
		processCtx: processCtx, processCancel: processCancel,
		newProcess: worker.NewProcessWithRequestHandler,
	}
}

func (e *lazyProcessExecutor) Execute(ctx context.Context, request worker.ExecuteRequest, _ worker.EventSink) (worker.ExecuteResult, error) {
	spec, ok := ctx.Value(workerExecutionContextKey{}).(workerExecutionSpec)
	if !ok {
		return worker.ExecuteResult{}, fmt.Errorf("worker execution metadata is missing")
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return worker.ExecuteResult{}, fmt.Errorf("worker executor is closed")
	}
	e.activeAssignmentID = request.AssignmentID
	e.activeRunID = request.RunID
	e.activeKind = spec.Kind
	e.mu.Unlock()
	defer e.clearActive(request.AssignmentID)

	assignmentCtx := withAssignment(ctx, request.AssignmentID, request.WorkerID)
	switch spec.Kind {
	case workerExecutionEntry:
		e.runtime.emitRun(assignmentCtx, spec.Params)
		if err := ctx.Err(); err != nil {
			return worker.ExecuteResult{}, err
		}
		return worker.ExecuteResult{}, nil
	case workerExecutionDelegated:
		return e.executeDelegated(assignmentCtx, request, spec)
	default:
		return worker.ExecuteResult{}, fmt.Errorf("unsupported worker execution kind %q", spec.Kind)
	}
}

func (e *lazyProcessExecutor) executeDelegated(ctx context.Context, request worker.ExecuteRequest, spec workerExecutionSpec) (worker.ExecuteResult, error) {
	childParams := spec.Params
	childParams.Options.WorkerContext = &methods.WorkerExecutionContext{
		WorkerID:      string(request.WorkerID),
		AssignmentID:  string(request.AssignmentID),
		RunID:         request.RunID,
		ProxyMessages: true,
	}
	process, err := e.processFor(childParams, string(request.AssignmentID))
	if err != nil {
		return worker.ExecuteResult{}, err
	}
	var capture *worker.Capture
	for attempt := 0; attempt < 2; attempt++ {
		capture = worker.NewCapture(worker.CaptureOptions{
			MaxTurns: spec.MaxTurns,
			Backend:  "worker_pool",
			Name:     firstNonEmpty(spec.ProfileKey, string(request.WorkerID)),
			Task:     spec.Task,
		})
		err = process.Start(ctx, childParams, func(event events.EnvelopeV2) {
			capture.ObserveV2(event)
			_ = e.runtime.bridgeWorkerEvent(ctx, spec.Parent, request, spec.ProfileKey, event)
		})
		if err == nil {
			break
		}
		if ctx.Err() != nil || processHealthy(process) || attempt > 0 {
			e.discardProcess(process)
			return worker.ExecuteResult{}, capture.FailureError(fmt.Sprintf("worker process error: %v", err))
		}
		e.discardProcess(process)
		process, err = e.processFor(childParams, string(request.AssignmentID))
		if err != nil {
			return worker.ExecuteResult{}, err
		}
	}
	if status := capture.FinishStatus(); status != "" && status != "completed" {
		return worker.ExecuteResult{}, capture.FailureError("worker assignment finished with status " + status)
	}
	output := capture.FinalText()
	if !worker.ReportUsable(output) {
		return worker.ExecuteResult{}, capture.FailureError("worker assignment returned an empty final report")
	}
	return worker.ExecuteResult{Output: output}, nil
}

func (r *Runtime) bridgeWorkerEvent(ctx context.Context, parent methods.ReplyParams, request worker.ExecuteRequest, profileKey string, child events.EnvelopeV2) error {
	if child.Type == events.EventFinish {
		return nil
	}
	payload := cloneEventPayload(child.Payload)
	payload["visibility"] = "worker_private"
	stream := child.Stream
	if stream != nil {
		next := *stream
		next.StreamID = "stream_" + string(request.AssignmentID) + "_" + stream.StreamID
		stream = &next
	}
	workerCtx := withAssignment(ctx, request.AssignmentID, request.WorkerID)
	return r.emitAgentEvent(workerCtx, parent, events.AgentRef{
		AgentID: string(request.WorkerID),
		Role:    events.AgentRoleWorker,
		Name:    profileKey,
	}, child.Type, stream, payload)
}

func (e *lazyProcessExecutor) processFor(params methods.ReplyParams, assignmentID string) (worker.Process, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, fmt.Errorf("worker executor is closed")
	}
	if e.process != nil {
		if processHealthy(e.process) {
			return e.process, nil
		}
		_ = e.process.Close(context.Background())
		e.process = nil
	}
	process, err := e.newProcess(e.processCtx, params, assignmentID, e.handleChildRequest)
	if err != nil {
		return nil, err
	}
	e.process = process
	return process, nil
}

type healthyProcess interface {
	Healthy() bool
}

func processHealthy(process worker.Process) bool {
	if process == nil {
		return false
	}
	health, ok := process.(healthyProcess)
	return !ok || health.Healthy()
}

// handleChildRequest is the trust boundary for delegated Worker communication.
// The child supplies only destination and payload; source identity and run are
// read from the currently active Assignment so a reused process cannot retain
// authority from its previous execution.
func (e *lazyProcessExecutor) handleChildRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if method != internalWorkerSendMethod && method != internalWorkerReceiveMethod {
		return e.runtime.callGateway(ctx, method, params)
	}
	arguments, token, err := workerBridgeRequest(params)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	runCtx := agenttoolsContext(e.activeRunID, e.activeAssignmentID, e.workerID)
	delegated := e.activeKind == workerExecutionDelegated
	e.mu.Unlock()
	if !delegated || !token.ProxyMessages || token.RunID != runCtx.RunID ||
		token.AssignmentID != runCtx.AssignmentID || token.WorkerID != runCtx.WorkerID ||
		runCtx.AssignmentID == "" || runCtx.RunID == "" {
		return nil, worker.ErrAssignmentNotActive
	}

	var (
		output  string
		callErr error
	)
	switch method {
	case internalWorkerSendMethod:
		output, callErr = e.runtime.executeWorkerSend(ctx, runCtx, protocoltools.Call{Arguments: arguments})
	case internalWorkerReceiveMethod:
		output, callErr = e.runtime.executeWorkerReceive(ctx, runCtx, protocoltools.Call{})
	}
	if callErr != nil {
		return nil, callErr
	}
	return json.RawMessage(output), nil
}

func workerBridgeRequest(params any) (map[string]any, methods.WorkerExecutionContext, error) {
	arguments, err := workerBridgeArguments(params)
	if err != nil {
		return nil, methods.WorkerExecutionContext{}, err
	}
	rawToken, ok := arguments["worker_context"]
	if !ok {
		return nil, methods.WorkerExecutionContext{}, worker.ErrAssignmentNotActive
	}
	raw, err := json.Marshal(rawToken)
	if err != nil {
		return nil, methods.WorkerExecutionContext{}, err
	}
	var token methods.WorkerExecutionContext
	if err := json.Unmarshal(raw, &token); err != nil {
		return nil, methods.WorkerExecutionContext{}, err
	}
	delete(arguments, "worker_context")
	return arguments, token, nil
}

func agenttoolsContext(runID string, assignmentID worker.AssignmentID, workerID worker.WorkerID) agenttools.ToolRunContext {
	return agenttools.ToolRunContext{
		RunID:        runID,
		AssignmentID: string(assignmentID),
		WorkerID:     string(workerID),
	}
}

func workerBridgeArguments(params any) (map[string]any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, err
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	return arguments, nil
}

func (e *lazyProcessExecutor) discardProcess(process worker.Process) {
	e.mu.Lock()
	if e.process == process {
		e.process = nil
	}
	e.mu.Unlock()
	_ = process.Close(context.Background())
}

func (e *lazyProcessExecutor) clearActive(assignmentID worker.AssignmentID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeAssignmentID == assignmentID {
		e.activeAssignmentID = ""
		e.activeRunID = ""
		e.activeKind = ""
	}
}

func (e *lazyProcessExecutor) Cancel(ctx context.Context, assignmentID worker.AssignmentID, reason string) error {
	e.mu.Lock()
	process := e.process
	runID := e.activeRunID
	delegated := e.activeAssignmentID == assignmentID && e.activeKind == workerExecutionDelegated
	e.mu.Unlock()
	if process == nil || !delegated || runID == "" {
		return nil
	}
	return process.Cancel(ctx, runID, reason)
}

func (e *lazyProcessExecutor) Pause(ctx context.Context, assignmentID worker.AssignmentID, reason string) error {
	e.mu.Lock()
	process := e.process
	runID := e.activeRunID
	delegated := e.activeAssignmentID == assignmentID && e.activeKind == workerExecutionDelegated
	e.mu.Unlock()
	if process == nil || !delegated || runID == "" {
		return nil
	}
	pausable, ok := process.(worker.PausableProcess)
	if !ok {
		return fmt.Errorf("worker process does not support pause")
	}
	return pausable.Pause(ctx, runID, reason)
}

func (e *lazyProcessExecutor) Resume(ctx context.Context, assignmentID worker.AssignmentID) error {
	e.mu.Lock()
	process := e.process
	runID := e.activeRunID
	delegated := e.activeAssignmentID == assignmentID && e.activeKind == workerExecutionDelegated
	e.mu.Unlock()
	if process == nil || !delegated || runID == "" {
		return nil
	}
	pausable, ok := process.(worker.PausableProcess)
	if !ok {
		return fmt.Errorf("worker process does not support resume")
	}
	return pausable.Resume(ctx, runID)
}

func (e *lazyProcessExecutor) Reset(ctx context.Context) error {
	e.mu.Lock()
	process := e.process
	e.process = nil
	e.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.Close(ctx)
}

func (e *lazyProcessExecutor) Close(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	process := e.process
	e.process = nil
	processCancel := e.processCancel
	e.mu.Unlock()
	if processCancel != nil {
		processCancel()
	}
	if process == nil {
		return nil
	}
	return process.Close(ctx)
}

func (e *lazyProcessExecutor) Healthy() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.closed && (e.process == nil || processHealthy(e.process))
}

func workerPoolSizeFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("RED_PANDA_WORKER_POOL_SIZE"))
	if raw == "" {
		return worker.DefaultPoolSize
	}
	size, err := strconv.Atoi(raw)
	if err != nil || size < 1 {
		return worker.DefaultPoolSize
	}
	if size > worker.MaxPoolSize {
		return worker.MaxPoolSize
	}
	return size
}
