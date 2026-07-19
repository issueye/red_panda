package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

type SessionService struct {
	repos     repository.Set
	store     sessionStore
	purge     PurgeService
	runtime   *runtimeclient.Client
	hub       *eventhub.Hub
	// packer is the shared model-context packer (docs/plans/2026-07-19-convergence-wave.md
	// Wave B Task B1). Injected by Set so SessionService and RunService share one instance.
	packer *SessionContextPacker
	// compactor owns the compact critical section + plan/summary logic
	// (docs/plans/2026-07-19-convergence-wave.md Wave B Task B2). Injected by Set
	// as a pointer so all SessionService value copies share the same compact gate.
	compactor *SessionCompactor
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
	return NewSessionServiceWithPacker(repos, runtime, hub, archiveDir, NewSessionContextPacker(repos))
}

// NewSessionServiceWithPacker constructs a SessionService with an explicit
// shared context packer. The compactor and store are built internally and
// shared between SessionService and the compactor (single source of truth for
// fullHistory / activeSummary reads). Used by Set
// (docs/plans/2026-07-19-convergence-wave.md Wave B Task B1/B2).
func NewSessionServiceWithPacker(
	repos repository.Set,
	runtime *runtimeclient.Client,
	hub *eventhub.Hub,
	archiveDir string,
	packer *SessionContextPacker,
) SessionService {
	store := newSessionStore(repos, archiveDir)
	compactor := NewSessionCompactor(repos, store, runtime, hub)
	return SessionService{
		repos: repos, store: store, purge: newPurgeService(repos, runtime, hub, archiveDir),
		runtime: runtime, hub: hub, packer: packer, compactor: compactor,
	}
}

// beginSessionCompact / pauseSessionForCompact / resumeRunsAfterCompact /
// resumeSessionAfterCompact / ensureOpenTasks were extracted into
// SessionCompactor (session_compactor.go, docs/plans/2026-07-19-convergence-wave.md
// Wave B Task B2). Tests should call them on s.compactor directly.

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
	_, err := s.purge.PurgeSessions([]string{sessionID}, "user_delete")
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

// Compact-related entry points delegate to SessionCompactor
// (docs/plans/2026-07-19-convergence-wave.md Wave B Task B2). Each method
// preserves its prior signature so callers (controllers, tests) are unchanged.

func (s SessionService) CompactPreview(sessionID string, req CompactPreviewRequest) (CompactPreviewResult, error) {
	return s.compactor.Preview(sessionID, req)
}

func (s SessionService) Compact(sessionID string, req CompactSessionRequest) (CompactSessionResult, error) {
	return s.compactor.Apply(sessionID, req)
}

func (s SessionService) CompactionState(sessionID string) (CompactionStateResult, error) {
	return s.compactor.State(sessionID)
}

func (s SessionService) Summaries(sessionID string) ([]SessionSummaryDTO, error) {
	return s.compactor.Summaries(sessionID)
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
	// Same packing as BuildModelConversation so Desktop ring matches model input (docs/48 C).
	modelContext, err := s.packer.LoadForAssembly(sessionID)
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
