package mcp

import (
	"errors"
	"testing"
	"time"
)

func TestIsStartupPhaseFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"spawn error plain", errors.New("exec: \"foobar\": executable file not found"), true},
		{"start timeout", errors.New("start timeout after 1000ms"), true},
		{"initialize failed", errors.New("initialize failed: timeout"), true},
		{"initialize non-json", errors.New("initialize failed: invalid stdout JSON: not-json"), true},
		{"tools/call failed business", errors.New("tools/call failed: timeout"), false},
		{"tools/call isError", errors.New("tools/call failed: permission denied by server"), false},
		{"bare message no prefix (treated as startup-phase)", errors.New("permission denied by server"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isStartupPhaseFailure(tc.err); got != tc.want {
				t.Fatalf("isStartupPhaseFailure(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestFailureDecisionDisablesAtLimit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	window := 60 * time.Second
	recent, disabled := failureDecision(nil, now, window, 3)
	if disabled || len(recent) != 1 {
		t.Fatalf("first failure: recent=%v disabled=%v", recent, disabled)
	}
	recent, disabled = failureDecision(recent, now.Add(1*time.Second), window, 3)
	if disabled || len(recent) != 2 {
		t.Fatalf("second failure: recent=%v disabled=%v", recent, disabled)
	}
	recent, disabled = failureDecision(recent, now.Add(2*time.Second), window, 3)
	if !disabled || len(recent) != 3 {
		t.Fatalf("third failure should disable: recent=%v disabled=%v", recent, disabled)
	}
}

func TestFailureDecisionPrunesExpiredEntries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	window := 60 * time.Second
	// Two failures long ago (outside window).
	recent := []time.Time{now.Add(-2 * time.Minute), now.Add(-90 * time.Second)}
	// A fresh failure should NOT disable because the old ones get pruned.
	recent, disabled := failureDecision(recent, now, window, 3)
	if disabled {
		t.Fatalf("pruned history should not disable: recent=%v", recent)
	}
	if len(recent) != 1 {
		t.Fatalf("expected only the new entry to remain, got %v", recent)
	}
}

func TestFailureDecisionLimitZeroNeverDisables(t *testing.T) {
	t.Parallel()
	now := time.Now()
	recent := []time.Time{}
	for i := 0; i < 10; i++ {
		var disabled bool
		recent, disabled = failureDecision(recent, now, 60*time.Second, 0)
		if disabled {
			t.Fatalf("limit=0 should never disable (iter %d)", i)
		}
	}
}

func TestFailureDecisionAtExactBoundary(t *testing.T) {
	t.Parallel()
	// limit=3 means the 3rd failure within the window disables.
	now := time.Now()
	window := 60 * time.Second
	recent := []time.Time{now.Add(-1 * time.Second), now.Add(-500 * time.Millisecond)}
	// Two prior entries plus this one = 3 → disable.
	_, disabled := failureDecision(recent, now, window, 3)
	if !disabled {
		t.Fatalf("at-limit boundary should disable")
	}
}

func TestEnvParsersFallbackToDefaults(t *testing.T) {
	t.Parallel()
	// Not setting env → defaults. This test does not touch env to avoid racing
	// with other tests; it just confirms the constants are sensible.
	if defaultCrashLimit != 3 {
		t.Fatalf("defaultCrashLimit changed: %d", defaultCrashLimit)
	}
	if defaultCrashWindow != 60*time.Second {
		t.Fatalf("defaultCrashWindow changed: %v", defaultCrashWindow)
	}
}
