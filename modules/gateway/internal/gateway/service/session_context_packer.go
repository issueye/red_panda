package service

import (
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

// SessionContextPacker assembles the model-facing transcript for a session.
//
// It is the single owner of model-context packing (docs/48 Wave C) and is
// shared between RunService (run start) and SessionService (context state
// inspection). Both services receive the same Packer instance from Set so the
// "session owns packing but run consumes it" cross-service coupling becomes an
// explicit dependency instead of a free-function call from another service
// (docs/plans/2026-07-19-convergence-wave.md Wave B Task B1).
type SessionContextPacker struct {
	repos repository.Set
}

// NewSessionContextPacker constructs a Packer backed by the given repository set.
func NewSessionContextPacker(repos repository.Set) *SessionContextPacker {
	return &SessionContextPacker{repos: repos}
}

// BuildModelConversation assembles the model-facing transcript for a session:
// optional in-place compaction system message + message tail trimmed by token
// budget. Visible UI history is unchanged; only this path compresses model
// context (docs/13, docs/45, docs/48).
func (p *SessionContextPacker) BuildModelConversation(sessionID string) ([]methods.Message, error) {
	stored, err := p.LoadForAssembly(sessionID)
	if err != nil {
		return nil, err
	}
	return assembleModelConversation(stored)
}

// LoadForAssembly reads the recent message tail plus any active compaction
// summary and applies the soft token-budget trim.
func (p *SessionContextPacker) LoadForAssembly(sessionID string) (storedSessionContext, error) {
	return loadModelContextForAssembly(p.repos, sessionID)
}

// ModelContext is a thin read-only accessor used by SessionService.ContextState
// to expose the assembled conversation plus the raw stored tail in one call.
// It avoids SessionService re-implementing the load+select path.
func (p *SessionContextPacker) ModelContext(sessionID string) (storedSessionContext, error) {
	return p.LoadForAssembly(sessionID)
}
