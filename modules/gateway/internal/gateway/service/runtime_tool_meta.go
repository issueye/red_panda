package service

import (
	"fmt"
	"strings"

	"redpanda/gateway/internal/gateway/repository"
)

// validateRuntimeToolMeta enforces the common Runtime → Gateway tool envelope:
// run_id and tool_call_id are always required. When requireSession is true
// (todo/goal/context), session_id must be present and exist. Memory tools keep
// requireSession=false so list/create can run with workspace-scoped ownership
// checks only (existing behavior).
func validateRuntimeToolMeta(repos repository.Set, runID, sessionID, toolCallID string, requireSession bool) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run_id is required")
	}
	if strings.TrimSpace(toolCallID) == "" {
		return fmt.Errorf("tool_call_id is required")
	}
	if !requireSession {
		return nil
	}
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	if _, err := repos.Sessions.Get(sessionID); err != nil {
		return fmt.Errorf("session not found")
	}
	return nil
}
