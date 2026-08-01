package worker

import "testing"

func TestWorkerTurnPolicyReservesAnalysisAndReportCapacity(t *testing.T) {
	if DefaultToolTurns != 32 {
		t.Fatalf("DefaultToolTurns = %d, want 32", DefaultToolTurns)
	}
	if got := RecommendedTurns(31); got != 47 {
		t.Fatalf("RecommendedTurns(31) = %d, want 47", got)
	}
	if got := EffectiveToolTurns(0, 31); got != 47 {
		t.Fatalf("EffectiveToolTurns(0, 31) = %d, want 47", got)
	}
	if got := EffectiveToolTurns(80, 31); got != 80 {
		t.Fatalf("explicit budget = %d, want 80", got)
	}
}
