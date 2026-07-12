package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

const compactPauseTimeout = 20 * time.Second

type SessionService struct {
	repos   repository.Set
	runtime *runtimeclient.Client
}

type SessionDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Title           string    `json:"title"`
	WorkspaceRoot   string    `json:"workspace_root"`
	WorkingDir      string    `json:"working_dir"`
	Status          string    `json:"status"`
	ParentID        string    `json:"parent_id,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	SourceSessionID string    `json:"source_session_id,omitempty"`
	ForkPointSeq    uint64    `json:"fork_point_seq,omitempty"`
	ForkPointRunID  string    `json:"fork_point_run_id,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	CreatedAt       time.Time `json:"created_at"`
}

type MessageDTO struct {
	ID        string                 `json:"id"`
	SessionID string                 `json:"session_id"`
	Role      string                 `json:"role"`
	Content   []methods.ContentBlock `json:"content"`
	Seq       uint64                 `json:"seq"`
	RunID     string                 `json:"run_id,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
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
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CompactSessionResult struct {
	Session          SessionDTO     `json:"session"`
	Compaction       CompactionDTO  `json:"compaction"`
	Summary          CompactSummary `json:"summary"`
	KeepTailMessages int            `json:"keep_tail_messages"`
	KeepTailTurns    int            `json:"keep_tail_turns"`
	// PausedRuns is how many active root runs were paused before summary.
	PausedRuns int `json:"paused_runs,omitempty"`
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

func NewSessionService(repos repository.Set, runtime *runtimeclient.Client) SessionService {
	return SessionService{repos: repos, runtime: runtime}
}

// pauseSessionForCompact stops the session's active root run and all subagents,
// then waits until the session is idle so history can be summarized consistently.
// When the runtime client is unavailable, active run records are force-finished.
func (s SessionService) pauseSessionForCompact(sessionID string) (int, error) {
	// Authoritative goal pause even when force-finish has no Runtime event.
	_ = NewGoalService(s.repos).PauseBySession(sessionID, "session_compact")

	active, err := s.repos.Runs.ListActiveBySession(sessionID, 50)
	if err != nil {
		return 0, err
	}
	if len(active) == 0 {
		return 0, nil
	}

	// Without a runtime client we cannot stop live workers; mark records cancelled
	// immediately so summary can proceed against a stable session snapshot.
	if s.runtime == nil {
		for _, run := range active {
			_ = s.repos.Runs.Finish(run.ID, "cancelled", "paused for session compact")
		}
		return len(active), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), compactPauseTimeout)
	defer cancel()

	for _, run := range active {
		if res, listErr := s.runtime.SubAgents(ctx, methods.SubAgentsParams{RunID: run.ID}); listErr == nil {
			for _, item := range res.Items {
				status := strings.ToLower(strings.TrimSpace(item.Status))
				if status != "running" && status != "waiting_permission" && status != "cancelling" {
					continue
				}
				_, _ = s.runtime.CancelSubAgent(ctx, methods.SubAgentCancelParams{
					RunID:      run.ID,
					SubAgentID: item.SubAgentID,
					Reason:     "session compact pause",
				})
			}
		}
		_ = s.runtime.Cancel(ctx, methods.CancelParams{
			RunID:  run.ID,
			Reason: "session compact pause",
		})
	}

	deadline := time.Now().Add(compactPauseTimeout)
	for time.Now().Before(deadline) {
		count, countErr := s.repos.Runs.CountActiveBySession(sessionID)
		if countErr != nil {
			return len(active), countErr
		}
		if count == 0 {
			return len(active), nil
		}
		select {
		case <-ctx.Done():
			// fall through to force-finish below
		case <-time.After(100 * time.Millisecond):
			continue
		}
		break
	}

	// Ensure compact is never blocked forever if finish events are delayed.
	remaining, listErr := s.repos.Runs.ListActiveBySession(sessionID, 50)
	if listErr != nil {
		return len(active), listErr
	}
	for _, run := range remaining {
		_ = s.repos.Runs.Finish(run.ID, "cancelled", "paused for session compact")
	}
	return len(active), nil
}

func (s SessionService) Create(name string, workspaceRoot string) (SessionDTO, error) {
	session, err := s.repos.Sessions.Create(name, workspaceRoot)
	if err != nil {
		return SessionDTO{}, err
	}
	return sessionDTO(session), nil
}

func (s SessionService) List() ([]SessionDTO, error) {
	rows, err := s.repos.Sessions.List(100)
	if err != nil {
		return nil, err
	}
	items := make([]SessionDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, sessionDTO(row))
	}
	return items, nil
}

func (s SessionService) Delete(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if err := s.repos.Sessions.SoftDelete(sessionID); err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("session not found")
		}
		return err
	}
	_ = s.repos.Todos.DeleteBySession(sessionID)
	return nil
}

func (s SessionService) History(sessionID string) ([]MessageDTO, error) {
	rows, err := s.repos.Messages.List(sessionID, 200)
	if err != nil {
		return nil, err
	}
	items := make([]MessageDTO, 0, len(rows))
	for _, row := range rows {
		dto, err := messageDTO(row)
		if err != nil {
			return nil, err
		}
		items = append(items, dto)
	}
	return items, nil
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
	return result, err
}

func (s SessionService) CompactPreview(sessionID string, req CompactPreviewRequest) (CompactPreviewResult, error) {
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		return CompactPreviewResult{}, err
	}
	// Pause only once per compact flow: preview may run alone; apply pauses again if needed.
	// Prefer pausing before reading history so mid-run writes do not race the transcript.
	paused, err := s.pauseSessionForCompact(sessionID)
	if err != nil {
		return CompactPreviewResult{}, fmt.Errorf("pause session for compact: %w", err)
	}
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
		PausedRuns: paused,
	}, nil
}

func (s SessionService) Compact(sessionID string, req CompactSessionRequest) (CompactSessionResult, error) {
	source, err := s.repos.Sessions.Get(sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	// Always pause again before apply: preview may have been skipped, or a new
	// run may have started between preview and apply.
	paused, err := s.pauseSessionForCompact(sessionID)
	if err != nil {
		return CompactSessionResult{}, fmt.Errorf("pause session for compact: %w", err)
	}
	plan, err := s.planCompaction(sessionID, req.SourceRange, req.KeepTailMessages, req.KeepTailTurns)
	if err != nil {
		return CompactSessionResult{}, err
	}
	summary := req.Summary
	if strings.TrimSpace(summary.Summary) == "" {
		built, _, buildErr := s.buildCompactSummary(sessionID, plan.SummarizeMessages, plan.StartSeq, plan.EndSeq, compactSummaryOptions{
			Mode:              req.Mode,
			ProviderProfileID: req.ProviderProfileID,
		})
		if buildErr != nil {
			return CompactSessionResult{}, buildErr
		}
		summary = built
	} else {
		summary = s.ensureOpenTasks(sessionID, summary)
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return CompactSessionResult{}, err
	}

	var result CompactSessionResult
	err = s.repos.DB.Transaction(func(tx *gorm.DB) error {
		txRepos := repository.NewSet(tx)
		if err := txRepos.Compactions.SupersedeAppliedInPlace(source.ID); err != nil {
			return err
		}
		compaction, err := txRepos.Compactions.Create(model.SessionCompaction{
			SourceSessionID: source.ID,
			TargetSessionID: source.ID,
			Status:          "applied",
			SourceStartSeq:  plan.StartSeq,
			SourceEndSeq:    plan.EndSeq,
			SummaryJSON:     string(summaryJSON),
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
			PausedRuns:       paused,
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

func sessionDTO(row model.Session) SessionDTO {
	return SessionDTO{
		ID:              row.ID,
		Name:            row.Name,
		Title:           row.Name,
		WorkspaceRoot:   row.WorkspaceRoot,
		WorkingDir:      row.WorkspaceRoot,
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
	return MessageDTO{
		ID:        row.ID,
		SessionID: row.SessionID,
		Role:      row.Role,
		Content:   content,
		Seq:       row.Seq,
		RunID:     row.RunID,
		CreatedAt: row.CreatedAt,
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
