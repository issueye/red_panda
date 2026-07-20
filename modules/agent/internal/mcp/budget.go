package mcp

// Crash/restart budget for MCP servers (docs/19 §7.7).
//
// When an MCP server repeatedly fails to start (spawn / open / initialize
// phase), the Manager auto-disables it for the Runtime process lifetime.
// Disabled servers are hidden from provider tool registration and rejected at
// ExecuteTool. tools/call business failures are NOT counted — only failures
// that indicate the server itself is broken.
//
// Threshold defaults: 3 startup failures within a 60s sliding window.
// Overridable via env RED_PANDA_MCP_CRASH_LIMIT / RED_PANDA_MCP_CRASH_WINDOW_MS.
// Set RED_PANDA_MCP_CRASH_LIMIT=0 to disable the feature entirely.

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCrashLimit  = 3
	defaultCrashWindow = 60 * time.Second
)

// serverFailure tracks startup-phase failures for one MCP server.
// Locked by Manager.mu; lifetime is the Manager (= Runtime process).
type serverFailure struct {
	// recent holds timestamps of startup-phase failures still inside the
	// sliding window. Old entries are pruned on each recordStartupFailure call.
	recent []time.Time
	// disabled is set true when the threshold is hit; it stays true for the
	// Manager lifetime (no Reset API yet — see docs/19 §7.7 "until config changes").
	disabled bool
}

// isStartupPhaseFailure reports whether err came from spawn / open / initialize
// (i.e., the server itself is broken), as opposed to a tools/call business error.
//
// mcpkit.Call wraps initialize failures with "initialize failed:" and returns
// spawn / start-timeout errors with no "tools/call failed:" prefix. Business
// call failures (including timeouts and isError) are wrapped with the prefix
// "tools/call failed:". A nil error is never a startup-phase failure.
func isStartupPhaseFailure(err error) bool {
	if err == nil {
		return false
	}
	return !strings.HasPrefix(err.Error(), "tools/call failed:")
}

// failureDecision applies the sliding-window budget rule as a pure function.
// Returns the updated recent slice and whether the server should be disabled.
//
//   - limit <= 0: budget disabled — never disable, do not accumulate.
//   - Otherwise: prune entries older than (now - window), append now, and
//     disable when the post-prune count reaches limit.
//
// The caller owns the slice storage; this function may return the same backing
// array filtered in place.
func failureDecision(recent []time.Time, now time.Time, window time.Duration, limit int) ([]time.Time, bool) {
	if limit <= 0 {
		return recent, false
	}
	cutoff := now.Add(-window)
	// Prune in place.
	kept := recent[:0]
	for _, t := range recent {
		if t.After(cutoff) || t.Equal(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	if len(kept) >= limit {
		return kept, true
	}
	return kept, false
}

func crashLimitFromEnv() int {
	if v := strings.TrimSpace(os.Getenv("RED_PANDA_MCP_CRASH_LIMIT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultCrashLimit
}

func crashWindowFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("RED_PANDA_MCP_CRASH_WINDOW_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultCrashWindow
}

// errDisabled is returned by ExecuteTool when the server has been auto-disabled.
// Kept unexported; only used internally to produce the user-visible error string.
var errDisabledTemplate = errors.New("disabled by crash budget")
