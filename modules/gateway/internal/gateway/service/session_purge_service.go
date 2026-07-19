package service

import (
	"context"
	"log"
	"time"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

// PurgeService owns cascade deletion of sessions (and, by extension, workspace
// cleanup). It was previously the unexported sessionLifecycle helper; it is now
// a first-class service because two consumers (SessionService.Delete and
// WorkspaceService.Remove) depend on it
// (docs/plans/2026-07-19-convergence-wave.md Wave B Task B3).
type PurgeService struct {
	repos   repository.Set
	store   sessionStore
	runtime *runtimeclient.Client
	hub     *eventhub.Hub
}

func newPurgeService(repos repository.Set, runtime *runtimeclient.Client, hub *eventhub.Hub, archiveDir string) PurgeService {
	return PurgeService{
		repos: repos, store: newSessionStore(repos, archiveDir), runtime: runtime, hub: hub,
	}
}

// PurgeSessions cancels active runs, archives each session to JSONL, hard-deletes
// the session and all cascade-linked rows, and broadcasts session_deleted for
// each id (docs/49). Returns the number of sessions actually deleted.
func (l PurgeService) PurgeSessions(sessionIDs []string, reason string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}
	active, err := l.repos.Runs.ListActiveBySessions(sessionIDs)
	if err != nil {
		return 0, err
	}
	if l.runtime != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, run := range active {
			if _, err := l.runtime.CancelRun(ctx, methods.RunCancelParams{RunID: run.ID, Reason: "session_deleted"}); err != nil {
				log.Printf("cancel run %s while deleting session: %v", run.ID, err)
			}
		}
	}
	// Archive to JSONL then hard-delete (docs/49).
	deleted, err := l.store.deleteSessions(sessionIDs, reason)
	if err != nil {
		return 0, err
	}
	for _, sessionID := range sessionIDs {
		broadcastSessionDeleted(l.hub, sessionID, reason)
	}
	return deleted, nil
}
