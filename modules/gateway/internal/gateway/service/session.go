package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

const compactPauseTimeout = 20 * time.Second

// Compact concurrency contract (docs/48 Wave A):
//  1. At most one Compact or CompactPreview critical section per session_id.
//  2. Root run may stay active; pause is DelegatedOnly (Worker assignments).
//  3. Apply always re-plans from fullHistory after pause (server truth).
//  4. This wave does not cancel root runs (auto-compact product path).
var errSessionCompactInProgress = errors.New("session compact already in progress")

// sessionCompactGate is heap-allocated so SessionService value copies share one lock.
type sessionCompactGate struct {
	mu         sync.Mutex
	compacting map[string]struct{}
}

type SessionService struct {
	repos     repository.Set
	store     sessionStore
	lifecycle sessionLifecycle
	runtime   *runtimeclient.Client
	hub       *eventhub.Hub
	// compactGate must be a pointer: Set/controllers copy SessionService by value.
	compactGate *sessionCompactGate
}

type compactPauseState struct {
	runIDs []string
	paused int
}

// SessionDTO is the single-path session wire shape (BREAKING: no title/working_dir aliases).
type SessionDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	WorkspaceRoot   string    `json:"workspace_root"`
	Status          string    `json:"status"`
	ParentID        string    `json:"parent_id,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	SourceSessionID string    `json:"source_session_id,omitempty"`
	ForkPointSeq    uint64    `json:"fork_point_seq,omitempty"`
	ForkPointRunID  string    `json:"fork_point_run_id,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	CreatedAt       time.Time `json:"created_at"`
}

type SessionPageDTO struct {
	Items      []SessionDTO `json:"items"`
	NextOffset int          `json:"next_offset,omitempty"`
	HasMore    bool         `json:"has_more"`
}

type MessageDTO struct {
	ID           string                 `json:"id"`
	SessionID    string                 `json:"session_id"`
	Role         string                 `json:"role"`
	Content      []methods.ContentBlock `json:"content"`
	Seq          uint64                 `json:"seq"`
	RunID        string                 `json:"run_id,omitempty"`
	AssignmentID string                 `json:"assignment_id,omitempty"`
	WorkerID     string                 `json:"worker_id,omitempty"`
	ProfileKey   string                 `json:"profile_key,omitempty"`
	Visibility   string                 `json:"visibility,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

type MessagePageDTO struct {
	Items        []MessageDTO `json:"items"`
	NextAfterSeq uint64       `json:"next_after_seq,omitempty"`
	HasMore      bool         `json:"has_more"`
}

type ForkPoint struct {
	MessageSeq uint64 `json:"message_seq,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	RootSeq    uint64 `json:"root_seq,omitempty"`
}

type ForkSessionRequest struct {
	Name      string    `json:"name"`
	ForkPoint ForkPoint `json:"fork_point"`
}

type LineageDTO struct {
	ID              string    `json:"id"`
	SourceSessionID string    `json:"source_session_id"`
	TargetSessionID string    `json:"target_session_id"`
	Operation       string    `json:"operation"`
	ForkPointSeq    uint64    `json:"fork_point_seq,omitempty"`
	ForkPointRunID  string    `json:"fork_point_run_id,omitempty"`
	SourceStartSeq  uint64    `json:"source_start_seq,omitempty"`
	SourceEndSeq    uint64    `json:"source_end_seq,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type ForkSessionResult struct {
	Session        SessionDTO `json:"session"`
	Lineage        LineageDTO `json:"lineage"`
	CopiedMessages int        `json:"copied_messages"`
}

type SourceRange struct {
	StartSeq uint64 `json:"start_seq,omitempty"`
	EndSeq   uint64 `json:"end_seq,omitempty"`
}

type CompactSummary struct {
	Summary          string   `json:"summary"`
	Decisions        []string `json:"decisions"`
	OpenTasks        []string `json:"open_tasks"`
	WorkspaceContext []string `json:"workspace_context"`
	Risks            []string `json:"risks"`
}

type CompactPreviewRequest struct {
	SourceRange      SourceRange `json:"source_range"`
	KeepTailMessages int         `json:"keep_tail_messages"`
	// KeepTailTurns keeps the last N user-led conversation rounds verbatim
	// (user message + following assistant replies). Preferred over raw message count.
	KeepTailTurns     int    `json:"keep_tail_turns"`
	Mode              string `json:"mode"` // auto | llm | local
	ProviderProfileID string `json:"provider_profile_id,omitempty"`
}

type CompactPreview struct {
	SourceSessionID  string         `json:"source_session_id"`
	SourceStartSeq   uint64         `json:"source_start_seq"`
	SourceEndSeq     uint64         `json:"source_end_seq"`
	KeepTailMessages int            `json:"keep_tail_messages"`
	KeepTailTurns    int            `json:"keep_tail_turns,omitempty"`
	SummaryMethod    string         `json:"summary_method,omitempty"` // llm | local
	Summary          CompactSummary `json:"summary"`
}

type CompactSessionRequest struct {
	Name              string         `json:"name"`
	SourceRange       SourceRange    `json:"source_range"`
	KeepTailMessages  int            `json:"keep_tail_messages"`
	KeepTailTurns     int            `json:"keep_tail_turns"`
	Mode              string         `json:"mode"`
	ProviderProfileID string         `json:"provider_profile_id,omitempty"`
	Summary           CompactSummary `json:"summary"`
}

type CompactionDTO struct {
	ID               string    `json:"id"`
	SourceSessionID  string    `json:"source_session_id"`
	TargetSessionID  string    `json:"target_session_id"`
	Status           string    `json:"status"`
	SourceStartSeq   uint64    `json:"source_start_seq"`
	SourceEndSeq     uint64    `json:"source_end_seq"`
	SummaryMessageID string    `json:"summary_message_id,omitempty"`
	SummaryMethod    string    `json:"summary_method,omitempty"`
	KeepTailMessages int       `json:"keep_tail_messages,omitempty"`
	KeepTailTurns    int       `json:"keep_tail_turns,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CompactSessionResult struct {
	Session          SessionDTO     `json:"session"`
	Compaction       CompactionDTO  `json:"compaction"`
	Summary          CompactSummary `json:"summary"`
	KeepTailMessages int            `json:"keep_tail_messages"`
	KeepTailTurns    int            `json:"keep_tail_turns"`
	// PausedRuns is the number of delegated Worker assignments paused before summary.
	PausedRuns    int    `json:"paused_runs,omitempty"`
	SummaryMethod string `json:"summary_method,omitempty"`
}

type CompactPreviewResult struct {
	Preview    CompactPreview `json:"preview"`
	PausedRuns int            `json:"paused_runs,omitempty"`
}

type CompactionStateResult struct {
	Active     bool           `json:"active"`
	Compaction *CompactionDTO `json:"compaction,omitempty"`
	Summary    CompactSummary `json:"summary"`
}

type SessionSummaryDTO struct {
	Compaction CompactionDTO  `json:"compaction"`
	Summary    CompactSummary `json:"summary"`
}

type SessionContextState struct {
	SessionID        string                `json:"session_id"`
	SummaryActive    bool                  `json:"summary_active"`
	ActiveSummary    *SessionSummaryDTO    `json:"active_summary,omitempty"`
	TailMessageCount int64                 `json:"tail_message_count"`
	GoalID           string                `json:"goal_id,omitempty"`
	ContextItems     []methods.GoalNoteDTO `json:"context_items"`
	Usage            ContextUsageDTO       `json:"usage"`
}

type ContextUsageDTO struct {
	EstimatedTokens   int    `json:"estimated_tokens"`
	ModelMessageCount int    `json:"model_message_count"`
	CoveredEndSeq     uint64 `json:"covered_end_seq"`
}

func NewSessionService(repos repository.Set, runtime *runtimeclient.Client, hub *eventhub.Hub, archiveDir string) SessionService {
	return SessionService{
		repos: repos, store: newSessionStore(repos, archiveDir), lifecycle: newSessionLifecycle(repos, runtime, hub, archiveDir),
		runtime: runtime, hub: hub,
		compactGate: &sessionCompactGate{compacting: make(map[string]struct{})},
	}
}

// beginSessionCompact acquires the per-session compact critical section.
// Caller must invoke the returned unlock (typically via defer).
func (s SessionService) beginSessionCompact(sessionID string) (unlock func(), err error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return func() {}, fmt.Errorf("session id is required")
	}
	gate := s.compactGate
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
func (s SessionService) pauseSessionForCompact(sessionID string) (compactPauseState, error) {
	active, err := s.repos.Runs.ListActiveBySession(sessionID, 50)
	if err != nil {
		return compactPauseState{}, err
	}
	if len(active) == 0 {
		return compactPauseState{}, nil
	}

	if s.runtime == nil {
		return compactPauseState{}, fmt.Errorf("runtime is required to pause active workers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), compactPauseTimeout)
	defer cancel()

	state := compactPauseState{runIDs: make([]string, 0, len(active))}
	for _, run := range active {
		state.runIDs = append(state.runIDs, run.ID)
		result, pauseErr := s.runtime.PauseRun(ctx, methods.RunPauseParams{
			RunID: run.ID, Reason: "session compact", DelegatedOnly: true,
		})
		if pauseErr != nil {
			resumeErr := s.resumeRunsAfterCompact(state.runIDs)
			return state, errors.Join(pauseErr, resumeErr)
		}
		state.paused += result.Paused
	}
	return state, nil
}

func (s SessionService) resumeRunsAfterCompact(runIDs []string) error {
	if s.runtime == nil || len(runIDs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), compactPauseTimeout)
	defer cancel()
	var resumeErr error
	for _, runID := range runIDs {
		_, err := s.runtime.ResumeRun(ctx, methods.RunResumeParams{RunID: runID, DelegatedOnly: true})
		if err != nil {
			resumeErr = errors.Join(resumeErr, fmt.Errorf("resume run %s: %w", runID, err))
		}
	}
	return resumeErr
}

func (s SessionService) resumeSessionAfterCompact(sessionID string) error {
	active, err := s.repos.Runs.ListActiveBySession(sessionID, 50)
	if err != nil {
		return err
	}
	runIDs := make([]string, 0, len(active))
	for _, run := range active {
		runIDs = append(runIDs, run.ID)
	}
	return s.resumeRunsAfterCompact(runIDs)
}

func (s SessionService) Create(name string, workspaceRoot string) (SessionDTO, error) {
	session, err := s.repos.Sessions.Create(name, workspaceRoot)
	if err != nil {
		return SessionDTO{}, err
	}
	broadcastSessionUpserted(s.hub, session, map[string]any{"reason": "user_create"})
	return sessionDTO(session), nil
}

func (s SessionService) List() ([]SessionDTO, error) {
	rows, err := s.repos.Sessions.ListAll()
	if err != nil {
		return nil, err
	}
	items := make([]SessionDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, sessionDTO(row))
	}
	return items, nil
}

func (s SessionService) ListPage(offset, limit int) (SessionPageDTO, error) {
	rows, hasMore, err := s.repos.Sessions.ListPage(offset, limit)
	if err != nil {
		return SessionPageDTO{}, err
	}
	items := make([]SessionDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, sessionDTO(row))
	}
	next := 0
	if hasMore {
		next = offset + len(items)
	}
	return SessionPageDTO{Items: items, NextOffset: next, HasMore: hasMore}, nil
}

func (s SessionService) Delete(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("session not found")
		}
		return err
	}
	_, err := s.lifecycle.delete([]string{sessionID}, "user_delete")
	return err
}

func (s SessionService) History(sessionID string, afterSeq uint64, limit int) (MessagePageDTO, error) {
	page, err := s.store.messagePage(sessionID, afterSeq, limit)
	if err != nil {
		return MessagePageDTO{}, err
	}
	items := make([]MessageDTO, 0, len(page.Messages))
	for _, row := range page.Messages {
		dto, err := messageDTO(row)
		if err != nil {
			return MessagePageDTO{}, err
		}
		items = append(items, dto)
	}
	next := uint64(0)
	if page.HasMore && len(items) > 0 {
		next = items[len(items)-1].Seq
	}
	return MessagePageDTO{Items: items, NextAfterSeq: next, HasMore: page.HasMore}, nil
}

func (s SessionService) Fork(sessionID string, req ForkSessionRequest) (ForkSessionResult, error) {
	source, err := s.repos.Sessions.Get(sessionID)
	if err != nil {
		return ForkSessionResult{}, err
	}
	forkPointSeq := req.ForkPoint.MessageSeq
	if forkPointSeq == 0 {
		forkPointSeq, err = s.repos.Messages.LatestSeq(sessionID)
		if err != nil {
			return ForkSessionResult{}, err
		}
	}
	messages, err := s.repos.Messages.ListThroughSeq(sessionID, forkPointSeq)
	if err != nil {
		return ForkSessionResult{}, err
	}
	forkPointRunID := req.ForkPoint.RunID
	if forkPointRunID == "" && len(messages) > 0 {
		forkPointRunID = messages[len(messages)-1].RunID
	}

	var result ForkSessionResult
	err = s.repos.DB.Transaction(func(tx *gorm.DB) error {
		txRepos := repository.NewSet(tx)
		target, err := txRepos.Sessions.CreateDerived(req.Name, source, "fork", forkPointSeq, forkPointRunID)
		if err != nil {
			return err
		}
		copied, err := txRepos.Messages.CopyToSession(messages, target.ID, 1)
		if err != nil {
			return err
		}
		if _, err := txRepos.Todos.CopySessionTodos(source.ID, target.ID, false); err != nil {
			return err
		}
		lineage, err := txRepos.Lineage.Create(model.SessionLineage{
			SourceSessionID: source.ID,
			TargetSessionID: target.ID,
			Operation:       "fork",
			ForkPointSeq:    forkPointSeq,
			ForkPointRunID:  forkPointRunID,
			SourceStartSeq:  firstMessageSeq(messages),
			SourceEndSeq:    lastMessageSeq(messages),
		})
		if err != nil {
			return err
		}
		result = ForkSessionResult{
			Session:        sessionDTO(target),
			Lineage:        lineageDTO(lineage),
			CopiedMessages: copied,
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if row, e := s.repos.Sessions.Get(result.Session.ID); e == nil {
		broadcastSessionUpserted(s.hub, row, map[string]any{"reason": "fork"})
	}
	return result, nil
}

func (s SessionService) CompactPreview(sessionID string, req CompactPreviewRequest) (result CompactPreviewResult, err error) {
	unlock, err := s.beginSessionCompact(sessionID)
	if err != nil {
		return CompactPreviewResult{}, err
	}
	defer unlock()
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		return CompactPreviewResult{}, err
	}
	// Pause only once per compact flow: preview may run alone; apply pauses again if needed.
	// Prefer pausing before reading history so mid-run writes do not race the transcript.
	paused, err := s.pauseSessionForCompact(sessionID)
	if err != nil {
		return CompactPreviewResult{}, fmt.Errorf("pause session for compact: %w", err)
	}
	defer func() {
		if resumeErr := s.resumeRunsAfterCompact(paused.runIDs); resumeErr != nil {
			err = errors.Join(err, fmt.Errorf("resume session after compact: %w", resumeErr))
		}
	}()
	plan, err := s.planCompaction(sessionID, req.SourceRange, req.KeepTailMessages, req.KeepTailTurns)
	if err != nil {
		return CompactPreviewResult{}, err
	}
	summary, method, err := s.buildCompactSummary(sessionID, plan.SummarizeMessages, plan.StartSeq, plan.EndSeq, compactSummaryOptions{
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

func (s SessionService) Compact(sessionID string, req CompactSessionRequest) (result CompactSessionResult, err error) {
	unlock, err := s.beginSessionCompact(sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	defer unlock()
	source, err := s.repos.Sessions.Get(sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	// Always pause again before apply: preview may have been skipped, or a new
	// run may have started between preview and apply. Re-plan after pause uses
	// server fullHistory (docs/48).
	paused, err := s.pauseSessionForCompact(sessionID)
	if err != nil {
		return CompactSessionResult{}, fmt.Errorf("pause session for compact: %w", err)
	}
	defer func() {
		if resumeErr := s.resumeRunsAfterCompact(paused.runIDs); resumeErr != nil {
			err = errors.Join(err, fmt.Errorf("resume session after compact: %w", resumeErr))
		}
	}()
	plan, err := s.planCompaction(sessionID, req.SourceRange, req.KeepTailMessages, req.KeepTailTurns)
	if err != nil {
		return CompactSessionResult{}, err
	}
	summary := req.Summary
	summaryMethod := "provided"
	if strings.TrimSpace(summary.Summary) == "" {
		built, method, buildErr := s.buildCompactSummary(sessionID, plan.SummarizeMessages, plan.StartSeq, plan.EndSeq, compactSummaryOptions{
			Mode:              req.Mode,
			ProviderProfileID: req.ProviderProfileID,
		})
		if buildErr != nil {
			return CompactSessionResult{}, buildErr
		}
		summary = built
		summaryMethod = method
	} else {
		summary = s.ensureOpenTasks(sessionID, summary)
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return CompactSessionResult{}, err
	}

	err = s.repos.DB.Transaction(func(tx *gorm.DB) error {
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

func (s SessionService) CompactionState(sessionID string) (CompactionStateResult, error) {
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		return CompactionStateResult{}, err
	}
	row, err := s.repos.Compactions.LatestAppliedInPlace(sessionID)
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

func (s SessionService) Summaries(sessionID string) ([]SessionSummaryDTO, error) {
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		return nil, err
	}
	rows, err := s.repos.Compactions.ListForSession(sessionID)
	if err != nil {
		return nil, err
	}
	items := make([]SessionSummaryDTO, 0, len(rows))
	for _, row := range rows {
		var summary CompactSummary
		if err := json.Unmarshal([]byte(row.SummaryJSON), &summary); err != nil {
			return nil, fmt.Errorf("decode compaction summary %s: %w", row.ID, err)
		}
		items = append(items, SessionSummaryDTO{Compaction: compactionDTO(row), Summary: summary})
	}
	return items, nil
}

func (s SessionService) ContextState(sessionID string) (SessionContextState, error) {
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		return SessionContextState{}, err
	}
	state := SessionContextState{SessionID: sessionID, ContextItems: []methods.GoalNoteDTO{}}
	summary, err := s.store.activeSummary(sessionID)
	if err != nil {
		return SessionContextState{}, err
	}
	coveredEnd := uint64(0)
	if summary != nil {
		var value CompactSummary
		if err := json.Unmarshal([]byte(summary.SummaryJSON), &value); err != nil {
			return SessionContextState{}, fmt.Errorf("decode active compaction summary: %w", err)
		}
		state.SummaryActive = true
		state.ActiveSummary = &SessionSummaryDTO{Compaction: compactionDTO(*summary), Summary: value}
		coveredEnd = summary.SourceEndSeq
	}
	state.TailMessageCount, err = s.repos.Messages.CountConversationAfterSeq(sessionID, coveredEnd)
	if err != nil {
		return SessionContextState{}, err
	}
	// Same packing as buildModelConversation so Desktop ring matches model input (docs/48 C).
	modelContext, err := loadModelContextForAssembly(s.repos, sessionID)
	if err != nil {
		return SessionContextState{}, err
	}
	state.Usage = estimateStoredContextUsage(modelContext, coveredEnd)
	goal, err := s.repos.Goals.GetActiveBySession(sessionID)
	if err == gorm.ErrRecordNotFound {
		goal, err = s.repos.Goals.GetLatestPausedBySession(sessionID)
	}
	if err == gorm.ErrRecordNotFound {
		goal, err = s.repos.Goals.GetLatestPendingBySession(sessionID)
	}
	if err == gorm.ErrRecordNotFound {
		return state, nil
	}
	if err != nil {
		return SessionContextState{}, err
	}
	state.GoalID = goal.ID
	notes, err := s.repos.Contexts.List(goal.ID, repository.NoteListOpts{Limit: contextMaxListLimit})
	if err != nil {
		return SessionContextState{}, err
	}
	for _, note := range notes {
		state.ContextItems = append(state.ContextItems, noteToDTO(note))
	}
	return state, nil
}

func estimateStoredContextUsage(context storedSessionContext, coveredEnd uint64) ContextUsageDTO {
	usage := ContextUsageDTO{CoveredEndSeq: coveredEnd, ModelMessageCount: len(context.Messages)}
	if context.Summary != nil {
		usage.ModelMessageCount++
		usage.EstimatedTokens += estimateContextTextTokens(context.Summary.SummaryJSON) + 8
	}
	for _, message := range context.Messages {
		usage.EstimatedTokens += estimateContextTextTokens(messageText(message)) + 4
	}
	return usage
}

func estimateContextTextTokens(text string) int {
	latinRunes := 0
	tokens := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			tokens++
		} else {
			latinRunes++
		}
	}
	return tokens + (latinRunes+3)/4
}

func sessionDTO(row model.Session) SessionDTO {
	return SessionDTO{
		ID:              row.ID,
		Name:            row.Name,
		WorkspaceRoot:   row.WorkspaceRoot,
		Status:          row.Status,
		ParentID:        row.ParentID,
		Kind:            row.Kind,
		SourceSessionID: row.SourceSessionID,
		ForkPointSeq:    row.ForkPointSeq,
		ForkPointRunID:  row.ForkPointRunID,
		UpdatedAt:       row.UpdatedAt,
		CreatedAt:       row.CreatedAt,
	}
}

func lineageDTO(row model.SessionLineage) LineageDTO {
	return LineageDTO{
		ID:              row.ID,
		SourceSessionID: row.SourceSessionID,
		TargetSessionID: row.TargetSessionID,
		Operation:       row.Operation,
		ForkPointSeq:    row.ForkPointSeq,
		ForkPointRunID:  row.ForkPointRunID,
		SourceStartSeq:  row.SourceStartSeq,
		SourceEndSeq:    row.SourceEndSeq,
		CreatedAt:       row.CreatedAt,
	}
}

func compactionDTO(row model.SessionCompaction) CompactionDTO {
	return CompactionDTO{
		ID:               row.ID,
		SourceSessionID:  row.SourceSessionID,
		TargetSessionID:  row.TargetSessionID,
		Status:           row.Status,
		SourceStartSeq:   row.SourceStartSeq,
		SourceEndSeq:     row.SourceEndSeq,
		SummaryMessageID: row.SummaryMessageID,
		SummaryMethod:    row.SummaryMethod,
		KeepTailMessages: row.KeepTailMessages,
		KeepTailTurns:    row.KeepTailTurns,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func messageDTO(row model.Message) (MessageDTO, error) {
	var content []methods.ContentBlock
	if row.ContentJSON != "" {
		if err := json.Unmarshal([]byte(row.ContentJSON), &content); err != nil {
			return MessageDTO{}, err
		}
	}
	var metadata struct {
		AssignmentID string `json:"assignment_id"`
		WorkerID     string `json:"worker_id"`
		ProfileKey   string `json:"profile_key"`
		Visibility   string `json:"visibility"`
	}
	if row.MetadataJSON != "" {
		if err := json.Unmarshal([]byte(row.MetadataJSON), &metadata); err != nil {
			return MessageDTO{}, err
		}
	}
	return MessageDTO{
		ID: row.ID, SessionID: row.SessionID, Role: row.Role, Content: content,
		Seq: row.Seq, RunID: row.RunID, AssignmentID: metadata.AssignmentID,
		WorkerID: metadata.WorkerID, ProfileKey: metadata.ProfileKey,
		Visibility: metadata.Visibility, CreatedAt: row.CreatedAt,
	}, nil
}

func (s SessionService) ensureOpenTasks(sessionID string, summary CompactSummary) CompactSummary {
	if len(summary.OpenTasks) > 0 {
		return summary
	}
	rows, err := s.repos.Todos.ListOpenBySession(sessionID)
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

func formatCompactSummaryMessage(summary CompactSummary) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(summary.Summary))
	writeSection := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString("\n\n")
		b.WriteString(title)
		b.WriteString(":\n")
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteByte('\n')
		}
	}
	writeSection("Decisions", summary.Decisions)
	writeSection("Open tasks", summary.OpenTasks)
	writeSection("Workspace context", summary.WorkspaceContext)
	writeSection("Risks", summary.Risks)
	return strings.TrimSpace(b.String())
}

func messageText(row model.Message) string {
	var content []methods.ContentBlock
	if row.ContentJSON == "" {
		return ""
	}
	if err := json.Unmarshal([]byte(row.ContentJSON), &content); err != nil {
		return ""
	}
	parts := make([]string, 0, len(content))
	for _, block := range content {
		if strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		}
	}
	return strings.Join(parts, " ")
}

func firstMessageSeq(messages []model.Message) uint64 {
	if len(messages) == 0 {
		return 0
	}
	return messages[0].Seq
}

func lastMessageSeq(messages []model.Message) uint64 {
	if len(messages) == 0 {
		return 0
	}
	return messages[len(messages)-1].Seq
}
