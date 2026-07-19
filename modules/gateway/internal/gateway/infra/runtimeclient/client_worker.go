package runtimeclient

// Worker and permission RPCs. Receivers stay on Client; this file only
// relocates them by domain (docs/plans/2026-07-19-convergence-wave.md Wave C Task C1).

import (
	"context"
	"encoding/json"

	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
)

func (c *Client) Workers(ctx context.Context, params methods.WorkerListParams) (methods.WorkerListResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.Workers(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerListResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerList, params)
	if err != nil {
		return methods.WorkerListResult{}, err
	}
	var result methods.WorkerListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerListResult{}, err
	}
	return result, nil
}

func (c *Client) CancelAssignment(ctx context.Context, params methods.WorkerAssignmentCancelParams) (methods.WorkerAssignmentCancelResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.CancelAssignment(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerAssignmentCancelResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerAssignmentCancel, params)
	if err != nil {
		return methods.WorkerAssignmentCancelResult{}, err
	}
	var result methods.WorkerAssignmentCancelResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerAssignmentCancelResult{}, err
	}
	return result, nil
}

func (c *Client) SendWorkerMessage(ctx context.Context, params methods.WorkerMessageSendParams) (methods.WorkerMessageSendResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerMessageSendResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerMessageSend, params)
	if err != nil {
		return methods.WorkerMessageSendResult{}, err
	}
	var result methods.WorkerMessageSendResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerMessageSendResult{}, err
	}
	return result, nil
}

func (c *Client) ReceiveWorkerMessage(ctx context.Context, params methods.WorkerMessageReceiveParams) (methods.WorkerMessageReceiveResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerMessageReceiveResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerMessageReceive, params)
	if err != nil {
		return methods.WorkerMessageReceiveResult{}, err
	}
	var result methods.WorkerMessageReceiveResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerMessageReceiveResult{}, err
	}
	return result, nil
}

func (c *Client) WorkerPoolStatus(ctx context.Context) (methods.WorkerPoolStatusResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerPoolStatusResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerPoolStatus, methods.WorkerPoolStatusParams{})
	if err != nil {
		return methods.WorkerPoolStatusResult{}, err
	}
	var result methods.WorkerPoolStatusResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerPoolStatusResult{}, err
	}
	return result, nil
}

func (c *Client) ResolvePermission(ctx context.Context, params permission.ResolveParams) (permission.ResolveResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.ResolvePermission(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return permission.ResolveResult{}, err
	}
	raw, err := c.call(ctx, methods.PermissionResolve, params)
	if err != nil {
		return permission.ResolveResult{}, err
	}
	var result permission.ResolveResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return permission.ResolveResult{}, err
	}
	return result, nil
}
