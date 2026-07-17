package service

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

// buildModelConversation assembles the model-facing transcript for a session:
// optional in-place compaction system message + message tail (or latest window).
// Visible UI history is unchanged; only this path compresses model context (docs/13, docs/45).
func buildModelConversation(repos repository.Set, sessionID string) ([]methods.Message, error) {
	history, err := repos.Messages.ListLatestConversation(sessionID, 200)
	if err != nil {
		return nil, err
	}
	conversation := make([]methods.Message, 0, len(history)+1)

	compaction, compactErr := repos.Compactions.LatestAppliedInPlace(sessionID)
	if compactErr == nil {
		var summary CompactSummary
		if err := json.Unmarshal([]byte(compaction.SummaryJSON), &summary); err != nil {
			return nil, fmt.Errorf("decode active compaction summary: %w", err)
		}
		tail, err := repos.Messages.ListConversationAfterSeq(sessionID, compaction.SourceEndSeq, 200)
		if err != nil {
			return nil, err
		}
		history = tail
		conversation = append(conversation, methods.Message{
			ID:   compaction.ID,
			Role: "system",
			Content: []methods.ContentBlock{{
				Type: "text",
				Text: fmt.Sprintf(
					"Conversation summary covering original messages %d-%d. Use it as prior context; the full original history remains stored in the session.\n\n%s",
					compaction.SourceStartSeq,
					compaction.SourceEndSeq,
					formatCompactSummaryMessage(summary),
				),
			}},
			CreatedAt: compaction.UpdatedAt.Format(time.RFC3339Nano),
		})
	} else if compactErr != gorm.ErrRecordNotFound {
		return nil, compactErr
	}

	for _, row := range history {
		message, err := messageDTO(row)
		if err != nil {
			return nil, err
		}
		conversation = append(conversation, methods.Message{
			ID:        message.ID,
			Role:      message.Role,
			Content:   message.Content,
			CreatedAt: message.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	return conversation, nil
}
