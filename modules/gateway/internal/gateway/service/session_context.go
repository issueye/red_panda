package service

import (
	"encoding/json"
	"fmt"
	"time"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

// Model-context packing (docs/48 Wave C). Aligned with Desktop SOFT_CONTEXT_BUDGET
// (tokenBudget.js): rough CJK/latin estimator, not a true tokenizer.
const (
	// modelContextFetchLimit is the max rows pulled from SQLite before token trim.
	// Higher than the old hard 200 so token budget can keep more short turns.
	modelContextFetchLimit = 800
	// defaultModelContextTokenBudget is the soft ceiling for model-facing transcript
	// packing when no provider max_tokens is available at assembly time.
	defaultModelContextTokenBudget = 32_000
	// minModelContextTokenBudget always leaves room for a small recent tail.
	minModelContextTokenBudget = 256
)

// buildModelConversation assembles the model-facing transcript for a session:
// optional in-place compaction system message + message tail trimmed by token budget.
// Visible UI history is unchanged; only this path compresses model context (docs/13, docs/45, docs/48).
func buildModelConversation(repos repository.Set, sessionID string) ([]methods.Message, error) {
	stored, err := loadModelContextForAssembly(repos, sessionID)
	if err != nil {
		return nil, err
	}
	return assembleModelConversation(stored)
}

func loadModelContextForAssembly(repos repository.Set, sessionID string) (storedSessionContext, error) {
	stored, err := newSessionStore(repos).modelContext(sessionID, modelContextFetchLimit)
	if err != nil {
		return storedSessionContext{}, err
	}
	reserved := 0
	if stored.Summary != nil {
		reserved = estimateContextTextTokens(stored.Summary.SummaryJSON) + 8
	}
	stored.Messages = selectModelContextMessages(stored.Messages, reserved, defaultModelContextTokenBudget)
	return stored, nil
}

func assembleModelConversation(stored storedSessionContext) ([]methods.Message, error) {
	history := stored.Messages
	conversation := make([]methods.Message, 0, len(history)+1)

	if stored.Summary != nil {
		compaction := *stored.Summary
		var summary CompactSummary
		if err := json.Unmarshal([]byte(compaction.SummaryJSON), &summary); err != nil {
			return nil, fmt.Errorf("decode active compaction summary: %w", err)
		}
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

// selectModelContextMessages keeps the newest messages that fit within
// (budget - reservedTokens). Order remains chronological (oldest→newest).
func selectModelContextMessages(messages []model.Message, reservedTokens, budget int) []model.Message {
	if len(messages) == 0 {
		return messages
	}
	if budget <= 0 {
		budget = defaultModelContextTokenBudget
	}
	remaining := budget - reservedTokens
	if remaining < minModelContextTokenBudget {
		remaining = minModelContextTokenBudget
	}

	used := 0
	start := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		cost := estimateContextTextTokens(messageText(messages[i])) + 4
		// Always include at least the newest message even if it alone exceeds budget.
		if used > 0 && used+cost > remaining {
			break
		}
		used += cost
		start = i
	}
	if start <= 0 {
		return messages
	}
	return messages[start:]
}
