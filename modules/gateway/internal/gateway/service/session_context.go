package service

import (
	"encoding/json"
	"fmt"
	"time"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

// Model-context packing constants (docs/48 Wave C).
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

// Model-context packing internals (docs/48 Wave C). The public entry points
// live on SessionContextPacker (session_context_packer.go); these helpers stay
// package-private so the Packer owns the surface while keeping the token-budget
// trim logic (selectModelContextMessages) reusable as a pure function.
//
// Aligned with Desktop SOFT_CONTEXT_BUDGET (tokenBudget.js): rough CJK/latin
// estimator, not a true tokenizer.

func loadModelContextForAssembly(repos repository.Set, sessionID string) (storedSessionContext, error) {
	stored, err := newSessionStore(repos, "").modelContext(sessionID, modelContextFetchLimit)
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
	// Prefer dropping oldest image_ref pixels when image token budget is
	// exceeded; keep text + alt placeholders (docs/51 §7.2 / docs/52 C5).
	dropExcessImagePixels(conversation, defaultImageTokenBudget)
	return conversation, nil
}

// defaultImageTokenBudget is the soft ceiling for image tokens inside one
// model conversation (roughly 4 medium screenshots).
const defaultImageTokenBudget = 4800

// dropExcessImagePixels walks oldest→newest and replaces overflowing image_ref
// blocks with text placeholders so Runtime does not rehydrate those pixels.
func dropExcessImagePixels(messages []methods.Message, budget int) {
	if budget <= 0 || len(messages) == 0 {
		return
	}
	// Compute total image tokens newest-first keep set.
	type imgRef struct{ mi, bi, tokens int }
	var images []imgRef
	for mi, msg := range messages {
		for bi, block := range msg.Content {
			if block.Type != "image_ref" {
				continue
			}
			images = append(images, imgRef{mi: mi, bi: bi, tokens: estimateImageTokens(block.Width, block.Height)})
		}
	}
	if len(images) == 0 {
		return
	}
	used := 0
	keep := make(map[int]map[int]bool, len(messages)) // mi -> bi -> keep
	// Walk newest first.
	for i := len(images) - 1; i >= 0; i-- {
		img := images[i]
		if used > 0 && used+img.tokens > budget {
			continue
		}
		used += img.tokens
		if keep[img.mi] == nil {
			keep[img.mi] = map[int]bool{}
		}
		keep[img.mi][img.bi] = true
	}
	for mi, msg := range messages {
		changed := false
		for bi, block := range msg.Content {
			if block.Type != "image_ref" {
				continue
			}
			if keep[mi] != nil && keep[mi][bi] {
				continue
			}
			// Replace with text placeholder; clear attachment id so Runtime
			// will not rehydrate pixels for this historical image.
			alt := block.Alt
			if alt == "" {
				alt = block.AttachmentID
			}
			if alt == "" {
				alt = block.Path
			}
			if alt == "" {
				alt = "image"
			}
			msg.Content[bi] = methods.ContentBlock{
				Type: "text",
				Text: "[image omitted: " + alt + "]",
			}
			changed = true
		}
		if changed {
			messages[mi] = msg
		}
	}
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
		cost := estimateMessageTokens(messages[i]) + 4
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
