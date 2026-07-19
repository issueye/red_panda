package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

// compactPauseTimeout bounds how long Compactor waits for Worker pause/resume
// RPCs during a compact critical section.
const compactPauseTimeout = 20 * time.Second

// errSessionCompactInProgress signals that another Compact or CompactPreview is
// already mid-flight for the same session_id.
var errSessionCompactInProgress = errors.New("session compact already in progress")

// Compact concurrency contract (docs/48 Wave A):
//  1. At most one Compact or CompactPreview critical section per session_id.
//  2. Root run may stay active; pause is DelegatedOnly (Worker assignments).
//  3. Apply always re-plans from fullHistory after pause (server truth).
//  4. This wave does not cancel root runs (auto-compact product path).
//
// sessionCompactGate is heap-allocated so SessionService/Compactor value copies
// share one lock.
type sessionCompactGate struct {
	mu         sync.Mutex
	compacting map[string]struct{}
}

type compactPauseState struct {
	runIDs []string
	paused int
}

// SessionCompactor owns the compact critical section, plan/summary logic, and
// pause/resume orchestration for in-place session compaction. Extracted from
// SessionService (docs/plans/2026-07-19-convergence-wave.md Wave B Task B2).
//
// The compactor is the single owner of the compact gate; SessionService value
// copies no longer need a shared compactGate pointer because they delegate to
// this struct (which itself is shared as a pointer via Set).
type SessionCompactor struct {
	repos   repository.Set
	store   sessionStore
	runtime *runtimeclient.Client
	hub     *eventhub.Hub
	gate    *sessionCompactGate
}

// NewSessionCompactor constructs a Compactor backed by the given collaborators.
// store must be the same sessionStore instance used by SessionService so both
// see the same fullHistory / activeSummary reads.
func NewSessionCompactor(repos repository.Set, store sessionStore, runtime *runtimeclient.Client, hub *eventhub.Hub) *SessionCompactor {
	return &SessionCompactor{
		repos:   repos,
		store:   store,
		runtime: runtime,
		hub:     hub,
		gate:    &sessionCompactGate{compacting: make(map[string]struct{})},
	}
}

// Preview runs the compact plan + summary build without persisting anything.
// Pauses DelegatedOnly Workers for the duration of the critical section.
func (c *SessionCompactor) Preview(sessionID string, req CompactPreviewRequest) (result CompactPreviewResult, err error) {
	unlock, err := c.beginSessionCompact(sessionID)
	if err != nil {
		return CompactPreviewResult{}, err
	}
	defer unlock()
	if _, err := c.repos.Sessions.Get(sessionID); err != nil {
		return CompactPreviewResult{}, err
	}
	// Pause only once per compact flow: preview may run alone; apply pauses again if needed.
	// Prefer pausing before reading history so mid-run writes do not race the transcript.
	paused, err := c.pauseSessionForCompact(sessionID)
	if err != nil {
		return CompactPreviewResult{}, fmt.Errorf("pause session for compact: %w", err)
	}
	defer func() {
		if resumeErr := c.resumeRunsAfterCompact(paused.runIDs); resumeErr != nil {
			err = errors.Join(err, fmt.Errorf("resume session after compact: %w", resumeErr))
		}
	}()
	plan, err := c.planCompaction(sessionID, req.SourceRange, req.KeepTailMessages, req.KeepTailTurns)
	if err != nil {
		return CompactPreviewResult{}, err
	}
	summary, method, err := c.buildCompactSummary(sessionID, plan.SummarizeMessages, plan.StartSeq, plan.EndSeq, compactSummaryOptions{
		Mode:              req.Mode,
		ProviderProfileID: req.ProviderProfileID,
	})
	if err != nil {
		return CompactPreviewResult{}, err
	}
	return CompactPreviewResult{
		Preview: CompactPreview{
			SourceSessionID:  sessionID,
			SourceStartSeq:   plan.StartSeq,
			SourceEndSeq:     plan.EndSeq,
			KeepTailMessages: plan.KeepTailMessages,
			KeepTailTurns:    plan.KeepTailTurns,
			SummaryMethod:    method,
			Summary:          summary,
		},
		PausedRuns: paused.paused,
	}, nil
}

// Apply persists a compaction row, superseding any prior applied-in-place
// compaction for the session. Always re-plans from fullHistory after pause so
// server truth wins over the client-supplied source range (docs/48).
func (c *SessionCompactor) Apply(sessionID string, req CompactSessionRequest) (result CompactSessionResult, err error) {
	unlock, err := c.beginSessionCompact(sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	defer unlock()
	source, err := c.repos.Sessions.Get(sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	// Always pause again before apply: preview may have been skipped, or a new
	// run may have started between preview and apply. Re-plan after pause uses
	// server fullHistory (docs/48).
	paused, err := c.pauseSessionForCompact(sessionID)
	if err != nil {
		return CompactSessionResult{}, fmt.Errorf("pause session for compact: %w", err)
	}
	defer func() {
		if resumeErr := c.resumeRunsAfterCompact(paused.runIDs); resumeErr != nil {
			err = errors.Join(err, fmt.Errorf("resume session after compact: %w", resumeErr))
		}
	}()
	plan, err := c.planCompaction(sessionID, req.SourceRange, req.KeepTailMessages, req.KeepTailTurns)
	if err != nil {
		return CompactSessionResult{}, err
	}
	summary := req.Summary
	summaryMethod := "provided"
	if strings.TrimSpace(summary.Summary) == "" {
		built, method, buildErr := c.buildCompactSummary(sessionID, plan.SummarizeMessages, plan.StartSeq, plan.EndSeq, compactSummaryOptions{
			Mode:              req.Mode,
			ProviderProfileID: req.ProviderProfileID,
		})
		if buildErr != nil {
			return CompactSessionResult{}, buildErr
		}
		summary = built
		summaryMethod = method
	} else {
		summary = c.ensureOpenTasks(sessionID, summary)
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return CompactSessionResult{}, err
	}

	err = c.repos.DB.Transaction(func(tx *gorm.DB) error {
		txRepos := repository.NewSet(tx)
		if err := txRepos.Compactions.SupersedeAppliedInPlace(source.ID); err != nil {
			return err
		}
		compaction, err := txRepos.Compactions.Create(model.SessionCompaction{
			SourceSessionID:  source.ID,
			TargetSessionID:  source.ID,
			Status:           "applied",
			SourceStartSeq:   plan.StartSeq,
			SourceEndSeq:     plan.EndSeq,
			SummaryJSON:      string(summaryJSON),
			SummaryMethod:    summaryMethod,
			KeepTailMessages: plan.KeepTailMessages,
			KeepTailTurns:    plan.KeepTailTurns,
		})
		if err != nil {
			return err
		}
		if err := txRepos.Sessions.Touch(source.ID); err != nil {
			return err
		}
		updated, err := txRepos.Sessions.Get(source.ID)
		if err != nil {
			return err
		}
		result = CompactSessionResult{
			Session:          sessionDTO(updated),
			Compaction:       compactionDTO(compaction),
			Summary:          summary,
			KeepTailMessages: plan.KeepTailMessages,
			KeepTailTurns:    plan.KeepTailTurns,
			PausedRuns:       paused.paused,
			SummaryMethod:    summaryMethod,
		}
		return nil
	})
	return result, err
}

// State returns the latest applied-in-place compaction for the session.
func (c *SessionCompactor) State(sessionID string) (CompactionStateResult, error) {
	if _, err := c.repos.Sessions.Get(sessionID); err != nil {
		return CompactionStateResult{}, err
	}
	row, err := c.repos.Compactions.LatestAppliedInPlace(sessionID)
	if err == gorm.ErrRecordNotFound {
		return CompactionStateResult{Active: false}, nil
	}
	if err != nil {
		return CompactionStateResult{}, err
	}
	var summary CompactSummary
	if err := json.Unmarshal([]byte(row.SummaryJSON), &summary); err != nil {
		return CompactionStateResult{}, fmt.Errorf("decode compaction summary: %w", err)
	}
	dto := compactionDTO(row)
	return CompactionStateResult{
		Active:     true,
		Compaction: &dto,
		Summary:    summary,
	}, nil
}

// Summaries lists all compaction snapshots recorded for the session.
func (c *SessionCompactor) Summaries(sessionID string) ([]SessionSummaryDTO, error) {
	if _, err := c.repos.Sessions.Get(sessionID); err != nil {
		return nil, err
	}
	rows, err := c.repos.Compactions.ListForSession(sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]SessionSummaryDTO, 0, len(rows))
	for _, row := range rows {
		var summary CompactSummary
		if err := json.Unmarshal([]byte(row.SummaryJSON), &summary); err != nil {
			return nil, fmt.Errorf("decode compaction summary: %w", err)
		}
		out = append(out, SessionSummaryDTO{Compaction: compactionDTO(row), Summary: summary})
	}
	return out, nil
}

// beginSessionCompact acquires the per-session compact critical section.
// Caller must invoke the returned unlock (typically via defer).
func (c *SessionCompactor) beginSessionCompact(sessionID string) (unlock func(), err error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return func() {}, fmt.Errorf("session id is required")
	}
	gate := c.gate
	if gate == nil {
		return func() {}, fmt.Errorf("session compact gate is not initialized")
	}
	gate.mu.Lock()
	if _, busy := gate.compacting[sessionID]; busy {
		gate.mu.Unlock()
		return func() {}, errSessionCompactInProgress
	}
	gate.compacting[sessionID] = struct{}{}
	gate.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.mu.Lock()
			delete(gate.compacting, sessionID)
			gate.mu.Unlock()
		})
	}, nil
}

// pauseSessionForCompact suspends delegated Workers while each main Worker and
// root Run remain alive. Paused Assignments retain their process and context.
func (c *SessionCompactor) pauseSessionForCompact(sessionID string) (compactPauseState, error) {
	active, err := c.repos.Runs.ListActiveBySession(sessionID, 50)
	if err != nil {
		return compactPauseState{}, err
	}
	if len(active) == 0 {
		return compactPauseState{}, nil
	}

	if c.runtime == nil {
		return compactPauseState{}, fmt.Errorf("runtime is required to pause active workers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), compactPauseTimeout)
	defer cancel()

	state := compactPauseState{runIDs: make([]string, 0, len(active))}
	for _, run := range active {
		state.runIDs = append(state.runIDs, run.ID)
		result, pauseErr := c.runtime.PauseRun(ctx, methods.RunPauseParams{
			RunID: run.ID, Reason: "session compact", DelegatedOnly: true,
		})
		if pauseErr != nil {
			resumeErr := c.resumeRunsAfterCompact(state.runIDs)
			return state, errors.Join(pauseErr, resumeErr)
		}
		state.paused += result.Paused
	}
	return state, nil
}

func (c *SessionCompactor) resumeRunsAfterCompact(runIDs []string) error {
	if c.runtime == nil || len(runIDs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), compactPauseTimeout)
	defer cancel()
	var resumeErr error
	for _, runID := range runIDs {
		_, err := c.runtime.ResumeRun(ctx, methods.RunResumeParams{RunID: runID, DelegatedOnly: true})
		if err != nil {
			resumeErr = errors.Join(resumeErr, fmt.Errorf("resume run %s: %w", runID, err))
		}
	}
	return resumeErr
}

func (c *SessionCompactor) resumeSessionAfterCompact(sessionID string) error {
	active, err := c.repos.Runs.ListActiveBySession(sessionID, 50)
	if err != nil {
		return err
	}
	runIDs := make([]string, 0, len(active))
	for _, run := range active {
		runIDs = append(runIDs, run.ID)
	}
	return c.resumeRunsAfterCompact(runIDs)
}

func (c *SessionCompactor) ensureOpenTasks(sessionID string, summary CompactSummary) CompactSummary {
	if len(summary.OpenTasks) > 0 {
		return summary
	}
	rows, err := c.repos.Todos.ListOpenBySession(sessionID)
	if err != nil || len(rows) == 0 {
		return summary
	}
	tasks := make([]string, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, row.Content)
	}
	summary.OpenTasks = tasks
	return summary
}
