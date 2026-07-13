package subagent

import (
	"context"
	"fmt"
	"strings"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

type ReleaseFunc func(reusable bool)

type ProcessProvider interface {
	Acquire(ctx context.Context, params methods.ReplyParams, subAgentID string, backend string) (Process, ReleaseFunc, error)
}

type EventSink interface {
	EmitStatus(ctx context.Context, spec RunSpec, status StatusEvent) error
	Bridge(ctx context.Context, spec RunSpec, child events.Envelope) error
}

type RunSpec struct {
	RootRunID        string
	SubAgentID       string
	Name             string
	DisplayName      string
	Backend          string
	Task             string
	FileCount        int
	ScopePath        string
	MaxTurns         int
	GoalPhase        string
	StartSummary     string
	CompletedSummary string
	Parent           methods.ReplyParams
	Child            methods.ReplyParams
}

type StatusEvent struct {
	Status  string
	Summary string
	Error   string
}

type RunResult struct {
	Text              string
	FinishStatus      string
	RecoveredFallback bool
}

type Coordinator struct {
	processes ProcessProvider
	events    EventSink
	registry  *Registry
}

func NewCoordinator(processes ProcessProvider, eventSink EventSink, registry *Registry) (*Coordinator, error) {
	if processes == nil {
		return nil, fmt.Errorf("subagent process provider is required")
	}
	if eventSink == nil {
		return nil, fmt.Errorf("subagent event sink is required")
	}
	if registry == nil {
		return nil, fmt.Errorf("subagent registry is required")
	}
	return &Coordinator{processes: processes, events: eventSink, registry: registry}, nil
}

func (c *Coordinator) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	if strings.TrimSpace(spec.RootRunID) == "" || strings.TrimSpace(spec.SubAgentID) == "" {
		return RunResult{}, fmt.Errorf("subagent root run id and subagent id are required")
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.registry.Register(Registration{
		SubAgentID:      spec.SubAgentID,
		Name:            spec.Name,
		Backend:         spec.Backend,
		RootRunID:       spec.RootRunID,
		ParentRunID:     spec.Parent.RunID,
		ParentSessionID: spec.Parent.Session.ID,
		ChildRunID:      spec.Child.RunID,
		Summary:         runningSummary(spec),
		Cancel:          cancel,
	})
	c.emitStatus(childCtx, spec, StatusEvent{Status: "running", Summary: runningSummary(spec)})

	child, release, err := c.processes.Acquire(childCtx, spec.Child, spec.SubAgentID, spec.Backend)
	if err != nil {
		return RunResult{}, c.fail(spec, fmt.Errorf("subagent process acquire failed: %w", err))
	}
	reusable := false
	defer func() {
		if release != nil {
			release(reusable)
		}
	}()

	capture := NewCapture(CaptureOptions{
		MaxTurns:  spec.MaxTurns,
		Backend:   spec.Backend,
		Name:      spec.Name,
		Task:      spec.Task,
		FileCount: spec.FileCount,
		ScopePath: spec.ScopePath,
	})
	err = child.Start(childCtx, spec.Child, func(event events.Envelope) {
		capture.Observe(event)
		_ = c.events.Bridge(context.Background(), spec, event)
	})
	if err != nil {
		if childCtx.Err() != nil {
			c.registry.Finish(spec.RootRunID, spec.SubAgentID, "cancelled", "subagent cancelled", childCtx.Err().Error())
			c.emitStatus(context.Background(), spec, StatusEvent{Status: "cancelled", Summary: "subagent cancelled", Error: childCtx.Err().Error()})
			return RunResult{}, childCtx.Err()
		}
		detail := capture.FailureError(fmt.Sprintf("subagent process error: %v", err))
		return RunResult{}, c.fail(spec, detail)
	}

	status := capture.FinishStatus()
	if status != "" && status != "completed" {
		detail := capture.FailureError(fmt.Sprintf("subagent finished with status %s", status))
		return RunResult{}, c.fail(spec, detail)
	}
	text := capture.FinalText()
	if text == "" {
		return RunResult{}, c.fail(spec, capture.FailureError("subagent returned an empty final report"))
	}
	if capture.RecoveredFallback() {
		return RunResult{}, c.fail(spec, capture.FailureError("subagent used a recovery fallback instead of a final report"))
	}
	if !ReportUsable(text) {
		return RunResult{}, c.fail(spec, capture.FailureError("subagent returned tool calls instead of a final report"))
	}

	reusable = true
	completedSummary := spec.CompletedSummary
	if strings.TrimSpace(completedSummary) == "" {
		completedSummary = "subagent completed"
	}
	c.registry.Finish(spec.RootRunID, spec.SubAgentID, "completed", completedSummary, "")
	c.emitStatus(context.Background(), spec, StatusEvent{Status: "completed", Summary: completedSummary})
	return RunResult{Text: text, FinishStatus: status, RecoveredFallback: capture.RecoveredFallback()}, nil
}

func (c *Coordinator) fail(spec RunSpec, err error) error {
	c.registry.Finish(spec.RootRunID, spec.SubAgentID, "failed", "subagent failed", err.Error())
	c.emitStatus(context.Background(), spec, StatusEvent{Status: "failed", Summary: "subagent failed", Error: err.Error()})
	return err
}

func (c *Coordinator) emitStatus(ctx context.Context, spec RunSpec, status StatusEvent) {
	_ = c.events.EmitStatus(ctx, spec, status)
}

func runningSummary(spec RunSpec) string {
	if strings.TrimSpace(spec.StartSummary) != "" {
		return spec.StartSummary
	}
	if strings.TrimSpace(spec.DisplayName) != "" {
		return spec.DisplayName + " started"
	}
	return "subagent started"
}
