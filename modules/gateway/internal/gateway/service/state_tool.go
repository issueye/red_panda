package service

import (
	"fmt"

	"redpanda/protocol/methods"
)

// DispatchStateTool routes the unified state.tool.execute envelope to the
// domain service that owns the tool (docs/41 W2-2 / W2-3). Domain services keep
// their typed result shapes so Runtime can unmarshal into Memory/Todo/Goal/
// ContextToolExecuteResult without a breaking payload change.
func (s Set) DispatchStateTool(params methods.StateToolExecuteParams) (any, error) {
	domain, err := methods.ResolveStateToolDomain(params.Domain, params.ToolName)
	if err != nil {
		return nil, err
	}
	switch domain {
	case methods.StateToolDomainMemory:
		return s.Memory.ExecuteRuntimeTool(params.AsMemoryParams())
	case methods.StateToolDomainTodo:
		return s.Todo.ExecuteRuntimeTool(params.AsTodoParams())
	case methods.StateToolDomainGoal:
		return s.Goal.ExecuteRuntimeTool(params.AsGoalParams())
	case methods.StateToolDomainContext:
		return s.Context.ExecuteRuntimeTool(params.AsContextParams())
	default:
		return nil, fmt.Errorf("unsupported state tool domain %q", domain)
	}
}

// RuntimeToolStatusCompleted is the shared success status for Gateway-mediated tools.
const RuntimeToolStatusCompleted = "completed"
