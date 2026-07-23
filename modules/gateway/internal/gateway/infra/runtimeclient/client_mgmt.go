package runtimeclient

// Management RPCs: MCP discovery and skill catalog operations.
// Receivers stay on Client; this file only relocates them by domain
// (docs/plans/2026-07-19-convergence-wave.md Wave C Task C1).

import (
	"context"
	"encoding/json"

	protocolmcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
)

func (c *Client) DiscoverMCP(ctx context.Context, params methods.MCPDiscoverParams) (protocolmcp.MCPDiscoveryResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return protocolmcp.MCPDiscoveryResult{}, err
	}
	raw, err := c.call(ctx, methods.MCPDiscover, params)
	if err != nil {
		return protocolmcp.MCPDiscoveryResult{}, err
	}
	var result protocolmcp.MCPDiscoveryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return protocolmcp.MCPDiscoveryResult{}, err
	}
	return result, nil
}

func (c *Client) CallMCP(ctx context.Context, params methods.MCPCallParams) (methods.MCPCallResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.MCPCallResult{}, err
	}
	raw, err := c.call(ctx, methods.MCPCall, params)
	if err != nil {
		return methods.MCPCallResult{}, err
	}
	var result methods.MCPCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.MCPCallResult{}, err
	}
	return result, nil
}

func (c *Client) ListSkills(ctx context.Context, params methods.SkillsListParams) (methods.SkillsListResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillsListResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkills, params)
	if err != nil {
		return methods.SkillsListResult{}, err
	}
	var result methods.SkillsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillsListResult{}, err
	}
	return result, nil
}

func (c *Client) LoadSkill(ctx context.Context, params methods.SkillLoadParams) (methods.SkillLoadResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillLoadResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkillLoad, params)
	if err != nil {
		return methods.SkillLoadResult{}, err
	}
	var result methods.SkillLoadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillLoadResult{}, err
	}
	return result, nil
}

func (c *Client) CreateSkill(ctx context.Context, params methods.SkillMutateParams) (methods.SkillMutateResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillMutateResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkillCreate, params)
	if err != nil {
		return methods.SkillMutateResult{}, err
	}
	var result methods.SkillMutateResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillMutateResult{}, err
	}
	return result, nil
}

func (c *Client) UpdateSkill(ctx context.Context, params methods.SkillMutateParams) (methods.SkillMutateResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillMutateResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkillUpdate, params)
	if err != nil {
		return methods.SkillMutateResult{}, err
	}
	var result methods.SkillMutateResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillMutateResult{}, err
	}
	return result, nil
}

func (c *Client) DeleteSkill(ctx context.Context, params methods.SkillDeleteParams) (methods.SkillDeleteResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillDeleteResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkillDelete, params)
	if err != nil {
		return methods.SkillDeleteResult{}, err
	}
	var result methods.SkillDeleteResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillDeleteResult{}, err
	}
	return result, nil
}
