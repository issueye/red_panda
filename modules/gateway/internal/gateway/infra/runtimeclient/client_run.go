package runtimeclient

// Run-lifecycle RPCs and per-run subprocess management.
// Method receivers stay on Client; this file only relocates them by domain
// (docs/plans/2026-07-19-convergence-wave.md Wave C Task C1).

import (
	"context"
	"encoding/json"
	"fmt"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

func (c *Client) Initialize(ctx context.Context) error {
	if err := c.ensureStarted(ctx); err != nil {
		return err
	}
	raw, err := c.call(ctx, methods.CoreInitialize, methods.InitializeParams{
		ProtocolVersion: events.ProtocolVersionV2,
		Client: methods.PeerInfo{
			Name:    "red-panda-gateway",
			Version: c.version,
		},
		Environment: methods.Environment{
			PermissionMode: "strict",
		},
		Capabilities: []methods.Capability{
			{Name: methods.RunExecute, Version: 1},
			{Name: methods.RunCancel, Version: 1},
			{Name: methods.RunPause, Version: 1},
			{Name: methods.RunResume, Version: 1},
			{Name: methods.WorkerList, Version: 1},
			{Name: methods.WorkerAssignmentCancel, Version: 1},
			{Name: methods.WorkerMessageSend, Version: 1},
			{Name: methods.WorkerMessageReceive, Version: 1},
			{Name: methods.WorkerPoolStatus, Version: 1},
		},
	})
	if err != nil {
		return err
	}
	var result methods.InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode runtime initialize result: %w", err)
	}
	if result.ProtocolVersion != events.ProtocolVersionV2 {
		return fmt.Errorf("runtime protocol version %q is incompatible with %q", result.ProtocolVersion, events.ProtocolVersionV2)
	}
	return nil
}

func (c *Client) Execute(ctx context.Context, params methods.RunExecuteParams) (methods.RunExecuteResult, error) {
	return c.ExecuteWithMode(ctx, "single_core", params)
}

func (c *Client) ExecuteWithMode(ctx context.Context, mode string, params methods.RunExecuteParams) (methods.RunExecuteResult, error) {
	if normalizedRuntimeMode(mode) == "per_run_process" {
		return c.executePerRun(ctx, params)
	}
	// For the shared core process, try to influence pool size before first start.
	c.setWorkerPoolSize(params.Options.WorkerPoolSize)
	if err := c.Initialize(ctx); err != nil {
		return methods.RunExecuteResult{}, err
	}
	raw, err := c.call(ctx, methods.RunExecute, params)
	if err != nil {
		return methods.RunExecuteResult{}, err
	}
	var accepted methods.RunExecuteResult
	if err := json.Unmarshal(raw, &accepted); err != nil {
		return methods.RunExecuteResult{}, err
	}
	return accepted, nil
}

func (c *Client) CancelRun(ctx context.Context, params methods.RunCancelParams) (methods.RunCancelResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.CancelRun(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.RunCancelResult{}, err
	}
	raw, err := c.call(ctx, methods.RunCancel, params)
	if err != nil {
		return methods.RunCancelResult{}, err
	}
	var result methods.RunCancelResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.RunCancelResult{}, err
	}
	return result, nil
}

func (c *Client) PauseRun(ctx context.Context, params methods.RunPauseParams) (methods.RunPauseResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.PauseRun(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.RunPauseResult{}, err
	}
	raw, err := c.call(ctx, methods.RunPause, params)
	if err != nil {
		return methods.RunPauseResult{}, err
	}
	var result methods.RunPauseResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.RunPauseResult{}, err
	}
	return result, nil
}

func (c *Client) ResumeRun(ctx context.Context, params methods.RunResumeParams) (methods.RunResumeResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.ResumeRun(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.RunResumeResult{}, err
	}
	raw, err := c.call(ctx, methods.RunResume, params)
	if err != nil {
		return methods.RunResumeResult{}, err
	}
	var result methods.RunResumeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.RunResumeResult{}, err
	}
	return result, nil
}

// executePerRun spawns a dedicated agent subprocess for a single run, bridges
// its events onto the parent event stream, and tears it down on terminal event
// or unexpected process exit.
func (c *Client) executePerRun(ctx context.Context, params methods.RunExecuteParams) (methods.RunExecuteResult, error) {
	if params.RunID == "" {
		return methods.RunExecuteResult{}, fmt.Errorf("run_id is required")
	}
	var child *Client
	child = New(c.command, c.args, c.version, func(event events.EnvelopeV2) {
		if c.onEvent != nil {
			c.onEvent(event)
		}
		// Release dedicated process on terminal events so multi-session slots free promptly.
		if event.RunID == params.RunID && (event.Type == events.EventFinish || event.Type == events.EventError) {
			// Detach synchronously so the process-exit callback cannot race a valid
			// terminal event and incorrectly turn a completed run into a failure.
			if c.removePerRun(params.RunID, child) {
				go func() { _ = child.Shutdown(context.Background()) }()
			}
		}
	}, c.onRequest)
	child.onExit = func(err error) {
		if !c.removePerRun(params.RunID, child) {
			return
		}
		c.mu.Lock()
		handler := c.onRunExit
		c.mu.Unlock()
		if handler != nil {
			handler(params.RunID, err)
		}
	}
	// Propagate Gateway-owned roots and other process configuration.
	c.mu.Lock()
	child.extraEnv = append([]string(nil), c.extraEnv...)
	c.mu.Unlock()
	child.setWorkerPoolSize(params.Options.WorkerPoolSize)
	c.mu.Lock()
	if _, exists := c.perRuns[params.RunID]; exists {
		c.mu.Unlock()
		return methods.RunExecuteResult{}, fmt.Errorf("per-run runtime already exists for %s", params.RunID)
	}
	c.perRuns[params.RunID] = child
	c.mu.Unlock()

	accepted, err := child.Execute(ctx, params)
	if err != nil {
		c.removePerRun(params.RunID, child)
		_ = child.Shutdown(context.Background())
		return methods.RunExecuteResult{}, err
	}
	return accepted, nil
}

func (c *Client) perRun(runID string) *Client {
	if runID == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.perRuns[runID]
}

func (c *Client) removePerRun(runID string, child *Client) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.perRuns[runID] != child {
		return false
	}
	delete(c.perRuns, runID)
	return true
}

func (c *Client) releasePerRun(runID string, child *Client) {
	if c.removePerRun(runID, child) {
		_ = child.Shutdown(context.Background())
	}
}
