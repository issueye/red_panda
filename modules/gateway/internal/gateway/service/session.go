package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

type SessionService struct {
	repos repository.Set
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
	Mode             string      `json:"mode"`
}

type CompactPreview struct {
	SourceSessionID  string         `json:"source_session_id"`
	SourceStartSeq   uint64         `json:"source_start_seq"`
	SourceEndSeq     uint64         `json:"source_end_seq"`
	KeepTailMessages int            `json:"keep_tail_messages"`
	Summary          CompactSummary `json:"summary"`
}

type CompactPreviewResult struct {
	Preview CompactPreview `json:"preview"`
}

type CompactSessionRequest struct {
	Name             string         `json:"name"`
	SourceRange      SourceRange    `json:"source_range"`
	KeepTailMessages int            `json:"keep_tail_messages"`
	Summary          CompactSummary `json:"summary"`
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
	Session            SessionDTO    `json:"session"`
	Lineage            LineageDTO    `json:"lineage"`
	Compaction         CompactionDTO `json:"compaction"`
	CopiedTailMessages int           `json:"copied_tail_messages"`
}

func NewSessionService(repos repository.Set) SessionService {
	return SessionService{repos: repos}
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
	startSeq, endSeq, keepTail, err := s.compactionRange(sessionID, req.SourceRange, req.KeepTailMessages)
	if err != nil {
		return CompactPreviewResult{}, err
	}
	messages, err := s.repos.Messages.ListRange(sessionID, startSeq, endSeq)
	if err != nil {
		return CompactPreviewResult{}, err
	}
	return CompactPreviewResult{
		Preview: CompactPreview{
			SourceSessionID:  sessionID,
			SourceStartSeq:   startSeq,
			SourceEndSeq:     endSeq,
			KeepTailMessages: keepTail,
			Summary:          summarizeMessages(messages, startSeq, endSeq),
		},
	}, nil
}

func (s SessionService) Compact(sessionID string, req CompactSessionRequest) (CompactSessionResult, error) {
	source, err := s.repos.Sessions.Get(sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	startSeq, endSeq, keepTail, err := s.compactionRange(sessionID, req.SourceRange, req.KeepTailMessages)
	if err != nil {
		return CompactSessionResult{}, err
	}
	summary := req.Summary
	if strings.TrimSpace(summary.Summary) == "" {
		messages, err := s.repos.Messages.ListRange(sessionID, startSeq, endSeq)
		if err != nil {
			return CompactSessionResult{}, err
		}
		summary = summarizeMessages(messages, startSeq, endSeq)
	}
	tailMessages, err := s.compactionTailMessages(sessionID, endSeq, keepTail)
	if err != nil {
		return CompactSessionResult{}, err
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return CompactSessionResult{}, err
	}
	metadataJSON := `{"synthetic":true,"kind":"compaction_summary"}`

	var result CompactSessionResult
	err = s.repos.DB.Transaction(func(tx *gorm.DB) error {
		txRepos := repository.NewSet(tx)
		target, err := txRepos.Sessions.CreateDerived(req.Name, source, "compact", endSeq, "")
		if err != nil {
			return err
		}
		summaryMessage, err := txRepos.Messages.AddWithMetadata(target.ID, "assistant", []methods.ContentBlock{{
			Type: "text",
			Text: summary.Summary,
		}}, "", metadataJSON)
		if err != nil {
			return err
		}
		copiedTail, err := txRepos.Messages.CopyToSession(tailMessages, target.ID, 2)
		if err != nil {
			return err
		}
		lineage, err := txRepos.Lineage.Create(model.SessionLineage{
			SourceSessionID: source.ID,
			TargetSessionID: target.ID,
			Operation:       "compact",
			SourceStartSeq:  startSeq,
			SourceEndSeq:    endSeq,
		})
		if err != nil {
			return err
		}
		compaction, err := txRepos.Compactions.Create(model.SessionCompaction{
			SourceSessionID:  source.ID,
			TargetSessionID:  target.ID,
			Status:           "applied",
			SourceStartSeq:   startSeq,
			SourceEndSeq:     endSeq,
			SummaryMessageID: summaryMessage.ID,
			SummaryJSON:      string(summaryJSON),
		})
		if err != nil {
			return err
		}
		result = CompactSessionResult{
			Session:            sessionDTO(target),
			Lineage:            lineageDTO(lineage),
			Compaction:         compactionDTO(compaction),
			CopiedTailMessages: copiedTail,
		}
		return nil
	})
	return result, err
}

func (s SessionService) compactionRange(sessionID string, requested SourceRange, keepTailMessages int) (uint64, uint64, int, error) {
	latestSeq, err := s.repos.Messages.LatestSeq(sessionID)
	if err != nil {
		return 0, 0, 0, err
	}
	startSeq := requested.StartSeq
	if startSeq == 0 {
		startSeq = 1
	}
	keepTail := keepTailMessages
	if keepTail < 0 {
		keepTail = 0
	}
	endSeq := requested.EndSeq
	if endSeq == 0 {
		endSeq = latestSeq
		if keepTail > 0 && uint64(keepTail) < latestSeq {
			endSeq = latestSeq - uint64(keepTail)
		}
	}
	if endSeq < startSeq {
		endSeq = startSeq
	}
	return startSeq, endSeq, keepTail, nil
}

func (s SessionService) compactionTailMessages(sessionID string, summarizedEndSeq uint64, keepTailMessages int) ([]model.Message, error) {
	if keepTailMessages <= 0 {
		return nil, nil
	}
	messages, err := s.repos.Messages.ListRange(sessionID, summarizedEndSeq+1, 0)
	if err != nil {
		return nil, err
	}
	if len(messages) <= keepTailMessages {
		return messages, nil
	}
	return messages[len(messages)-keepTailMessages:], nil
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

func summarizeMessages(messages []model.Message, startSeq uint64, endSeq uint64) CompactSummary {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		text := messageText(message)
		if text != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", message.Role, text))
		}
	}
	body := strings.Join(parts, " ")
	if len(body) > 320 {
		body = body[:317] + "..."
	}
	if body == "" {
		body = "No message content in selected range."
	}
	return CompactSummary{
		Summary: fmt.Sprintf("Compacted session messages %d-%d. %s", startSeq, endSeq, body),
	}
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
