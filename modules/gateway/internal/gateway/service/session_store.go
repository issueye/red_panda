package service

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

// sessionStore is the thin persistence projection for session history,
// summaries, and model-facing context. Repositories remain the SQLite details.
type sessionStore struct {
	repos      repository.Set
	archiveDir string
}

type storedMessagePage struct {
	Messages []model.Message
	HasMore  bool
}

type storedSessionContext struct {
	Summary  *model.SessionCompaction
	Messages []model.Message
}

func newSessionStore(repos repository.Set, archiveDir string) sessionStore {
	return sessionStore{repos: repos, archiveDir: strings.TrimSpace(archiveDir)}
}

func (s sessionStore) messagePage(sessionID string, afterSeq uint64, limit int) (storedMessagePage, error) {
	rows, hasMore, err := s.repos.Messages.ListPage(sessionID, afterSeq, limit)
	return storedMessagePage{Messages: rows, HasMore: hasMore}, err
}

func (s sessionStore) fullHistory(sessionID string) ([]model.Message, error) {
	return s.repos.Messages.ListAll(sessionID)
}

func (s sessionStore) activeSummary(sessionID string) (*model.SessionCompaction, error) {
	row, err := s.repos.Compactions.LatestAppliedInPlace(sessionID)
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s sessionStore) modelContext(sessionID string, limit int) (storedSessionContext, error) {
	summary, err := s.activeSummary(sessionID)
	if err != nil {
		return storedSessionContext{}, err
	}
	if summary == nil {
		rows, err := s.repos.Messages.ListLatestConversation(sessionID, limit)
		return storedSessionContext{Messages: rows}, err
	}
	rows, err := s.repos.Messages.ListConversationAfterSeq(sessionID, summary.SourceEndSeq, limit)
	return storedSessionContext{Summary: summary, Messages: rows}, err
}

// deleteSessions archives each session to JSONL then hard-deletes DB rows (docs/49).
// Archive failure aborts before any hard delete for that batch.
func (s sessionStore) deleteSessions(sessionIDs []string, reason string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}
	if reason == "" {
		reason = "user_delete"
	}
	for _, id := range sessionIDs {
		if _, err := archiveSessionJSONL(s.repos, s.archiveDir, id, reason); err != nil {
			return 0, fmt.Errorf("archive session %s before delete: %w", id, err)
		}
	}

	var deleted int64
	err := s.repos.DB.Transaction(func(tx *gorm.DB) error {
		repos := repository.NewSet(tx)
		// Cancel/disable soft state first for consistency with concurrent readers.
		if _, err := repos.Runs.CancelActiveBySessions(sessionIDs, "session deleted"); err != nil {
			return err
		}
		n, err := repos.HardDeleteCascade(sessionIDs)
		if err != nil {
			return err
		}
		deleted = n
		return nil
	})
	return deleted, err
}
