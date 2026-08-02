package service

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// SessionBootstrapResult is a single-round-trip hydrate payload for Desktop.
// Streams is the run-event transcript projection; history remains the
// model-facing message store.
type SessionBootstrapResult struct {
	History     MessagePageDTO         `json:"history"`
	Streams     []RunMessageStreamDTO  `json:"streams"`
	Runs        []RunRecordDTO         `json:"runs"`
	Tools       []ToolCallDTO          `json:"tools"`
	Permissions []PermissionRequestDTO `json:"permissions"`
	Todos       TodoListHTTPResult     `json:"todos"`
	Context     SessionContextState    `json:"context"`
}

// HistoryAll returns the full visible message list for a session (no client paging).
// Used by bootstrap so Desktop avoids multi-round history fetch.
func (s SessionService) HistoryAll(sessionID string) (MessagePageDTO, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return MessagePageDTO{}, fmt.Errorf("session id is required")
	}
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		if err == gorm.ErrRecordNotFound {
			return MessagePageDTO{}, fmt.Errorf("session not found")
		}
		return MessagePageDTO{}, err
	}
	rows, err := s.store.fullHistory(sessionID)
	if err != nil {
		return MessagePageDTO{}, err
	}
	items := make([]MessageDTO, 0, len(rows))
	for _, row := range rows {
		dto, err := messageDTO(row)
		if err != nil {
			return MessagePageDTO{}, err
		}
		items = append(items, dto)
	}
	return MessagePageDTO{Items: items, HasMore: false}, nil
}
