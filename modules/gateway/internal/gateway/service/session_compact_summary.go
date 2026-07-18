package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"redpanda/gateway/internal/gateway/model"
)

const (
	defaultKeepTailTurns    = 3
	maxLocalSummaryRunes    = 2400
	maxLocalPerMessageRunes = 280
	maxLLMTranscriptRunes   = 48_000
	compactLLMTimeout       = 45 * time.Second
	compactLLMMaxTokens     = 1200
)

type compactSummaryOptions struct {
	Mode              string
	ProviderProfileID string
}

type compactionPlan struct {
	StartSeq          uint64
	EndSeq            uint64
	KeepTailMessages  int
	KeepTailTurns     int
	SummarizeMessages []model.Message
	TailMessages      []model.Message
}

// planCompaction splits session history into "summarize older" + "keep recent".
// Prefer keep_tail_turns (conversation rounds) over raw keep_tail_messages.
func (s SessionService) planCompaction(sessionID string, requested SourceRange, keepTailMessages, keepTailTurns int) (compactionPlan, error) {
	all, err := s.store.fullHistory(sessionID)
	if err != nil {
		return compactionPlan{}, err
	}
	if len(all) == 0 {
		return compactionPlan{}, fmt.Errorf("session has no messages to compact")
	}

	// Explicit source_range end still supported for advanced callers.
	if requested.EndSeq > 0 || (keepTailTurns <= 0 && keepTailMessages > 0) {
		return s.planCompactionByMessageCount(all, requested, keepTailMessages)
	}

	turns := keepTailTurns
	if turns <= 0 {
		turns = defaultKeepTailTurns
	}
	return planCompactionByTurns(all, requested.StartSeq, turns), nil
}

func (s SessionService) planCompactionByMessageCount(all []model.Message, requested SourceRange, keepTailMessages int) (compactionPlan, error) {
	latestSeq := all[len(all)-1].Seq
	startSeq := requested.StartSeq
	if startSeq == 0 {
		startSeq = firstMessageSeq(all)
		if startSeq == 0 {
			startSeq = 1
		}
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

	var summarize []model.Message
	var tail []model.Message
	for _, msg := range all {
		if msg.Seq >= startSeq && msg.Seq <= endSeq {
			summarize = append(summarize, msg)
		}
		if msg.Seq > endSeq {
			tail = append(tail, msg)
		}
	}
	if keepTail > 0 && len(tail) > keepTail {
		tail = tail[len(tail)-keepTail:]
	}
	return compactionPlan{
		StartSeq:          startSeq,
		EndSeq:            endSeq,
		KeepTailMessages:  keepTail,
		SummarizeMessages: summarize,
		TailMessages:      tail,
	}, nil
}

// planCompactionByTurns keeps the last N user-led rounds and summarizes the rest.
func planCompactionByTurns(all []model.Message, startSeq uint64, turns int) compactionPlan {
	if turns < 1 {
		turns = defaultKeepTailTurns
	}
	// Find indices of user messages (root conversation only).
	userIdx := make([]int, 0, len(all))
	for i, msg := range all {
		if msg.Role == "user" {
			userIdx = append(userIdx, i)
		}
	}

	var tailStart int
	if len(userIdx) == 0 {
		// No user turns: keep last few messages raw.
		keep := turns * 2
		if keep > len(all) {
			keep = len(all)
		}
		tailStart = len(all) - keep
	} else if len(userIdx) <= turns {
		// Everything is "recent" — still leave the earliest slice for a short summary when possible.
		if len(userIdx) == 1 && userIdx[0] == 0 {
			// Only one turn covering whole history: summarize nothing meaningful; keep all.
			return compactionPlan{
				StartSeq:          firstMessageSeq(all),
				EndSeq:            firstMessageSeq(all),
				KeepTailTurns:     turns,
				KeepTailMessages:  len(all) - 1,
				SummarizeMessages: all[:1],
				TailMessages:      all[1:],
			}
		}
		// Keep the last turn fully; summarize earlier partial history if any.
		tailStart = userIdx[0]
		if len(userIdx) > 1 {
			tailStart = userIdx[len(userIdx)-1]
		}
	} else {
		tailStart = userIdx[len(userIdx)-turns]
	}
	if tailStart < 0 {
		tailStart = 0
	}
	if tailStart > len(all) {
		tailStart = len(all)
	}

	summarize := all[:tailStart]
	tail := all[tailStart:]
	if startSeq > 0 {
		filtered := summarize[:0]
		for _, msg := range summarize {
			if msg.Seq >= startSeq {
				filtered = append(filtered, msg)
			}
		}
		summarize = filtered
	}

	start := firstMessageSeq(summarize)
	end := lastMessageSeq(summarize)
	if start == 0 && len(all) > 0 {
		start = all[0].Seq
	}
	if end == 0 && len(summarize) == 0 && len(tail) > 0 {
		// Nothing to summarize: fabricate empty range before tail.
		if tail[0].Seq > 1 {
			end = tail[0].Seq - 1
			start = end
		} else {
			start, end = tail[0].Seq, tail[0].Seq
		}
	}

	return compactionPlan{
		StartSeq:          start,
		EndSeq:            end,
		KeepTailTurns:     turns,
		KeepTailMessages:  len(tail),
		SummarizeMessages: summarize,
		TailMessages:      tail,
	}
}

func (s SessionService) buildCompactSummary(
	sessionID string,
	messages []model.Message,
	startSeq uint64,
	endSeq uint64,
	opts compactSummaryOptions,
) (CompactSummary, string, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		mode = "auto"
	}

	tryLLM := mode == "auto" || mode == "llm" || mode == "summary"
	forceLocal := mode == "local"

	if tryLLM && !forceLocal {
		summary, err := s.summarizeMessagesWithLLM(messages, startSeq, endSeq, opts.ProviderProfileID)
		if err == nil && strings.TrimSpace(summary.Summary) != "" {
			return s.ensureOpenTasks(sessionID, summary), "llm", nil
		}
		if mode == "llm" {
			// Explicit LLM mode: surface the error instead of silently truncating.
			return CompactSummary{}, "", fmt.Errorf("llm compact summary failed: %w", err)
		}
	}

	summary := summarizeMessagesLocal(messages, startSeq, endSeq)
	return s.ensureOpenTasks(sessionID, summary), "local", nil
}

// summarizeMessagesLocal is a structured heuristic fallback (not a hard one-shot cut).
// It samples user/assistant turns with per-message budgets so older intent is retained.
func summarizeMessagesLocal(messages []model.Message, startSeq uint64, endSeq uint64) CompactSummary {
	type bullet struct {
		role string
		text string
	}
	var bullets []bullet
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		text := strings.TrimSpace(messageText(message))
		if text == "" {
			continue
		}
		bullets = append(bullets, bullet{role: message.Role, text: truncateRunes(text, maxLocalPerMessageRunes)})
	}

	if len(bullets) == 0 {
		return CompactSummary{
			Summary: fmt.Sprintf("Compacted session messages %d-%d (no root conversation content).", startSeq, endSeq),
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Session history summary (messages %d–%d, local heuristic):\n", startSeq, endSeq))
	used := 0
	for _, item := range bullets {
		line := fmt.Sprintf("- [%s] %s\n", item.role, item.text)
		lineRunes := utf8.RuneCountInString(line)
		if used+lineRunes > maxLocalSummaryRunes {
			b.WriteString("- …(earlier turns truncated in local fallback; enable LLM compact for fuller summary)\n")
			break
		}
		b.WriteString(line)
		used += lineRunes
	}

	// Light extraction of path-like tokens for workspace context.
	texts := make([]string, 0, len(bullets))
	for _, item := range bullets {
		texts = append(texts, item.text)
	}
	workspace := extractPathHintsFromTexts(texts)
	return CompactSummary{
		Summary:          strings.TrimSpace(b.String()),
		WorkspaceContext: workspace,
	}
}

func extractPathHintsFromTexts(texts []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, text := range texts {
		for _, token := range strings.Fields(text) {
			clean := strings.Trim(token, "`,.\"'()[]{}")
			if !looksLikePath(clean) {
				continue
			}
			if _, ok := seen[clean]; ok {
				continue
			}
			seen[clean] = struct{}{}
			out = append(out, clean)
			if len(out) >= 12 {
				return out
			}
		}
	}
	return out
}

func looksLikePath(value string) bool {
	if len(value) < 3 || len(value) > 180 {
		return false
	}
	if strings.Contains(value, "://") {
		return false
	}
	if strings.ContainsAny(value, `/\`) {
		return true
	}
	// common relative paths like src/foo.go
	if strings.Count(value, ".") >= 1 && strings.Contains(value, "/") {
		return true
	}
	return false
}

func (s SessionService) summarizeMessagesWithLLM(
	messages []model.Message,
	startSeq uint64,
	endSeq uint64,
	providerProfileID string,
) (CompactSummary, error) {
	profile, err := s.resolveCompactProvider(providerProfileID)
	if err != nil {
		return CompactSummary{}, err
	}
	transcript := buildCompactTranscript(messages, maxLLMTranscriptRunes)
	if strings.TrimSpace(transcript) == "" {
		return CompactSummary{}, fmt.Errorf("no transcript content to summarize")
	}

	system := `You compress multi-turn coding-agent conversations into durable context.
Return ONLY a JSON object with keys:
- summary: string (2-8 sentences; goals, progress, current state)
- decisions: string[] (decisions / chosen approaches)
- open_tasks: string[] (unfinished work)
- workspace_context: string[] (important files, commands, env notes)
- risks: string[] (blockers, bugs, caveats)
Rules:
- Prefer facts useful for continuing work over narrative fluff.
- Do not invent files, APIs, or outcomes not present in the transcript.
- Ignore subagent-private chatter if present.
- Keep paths and error messages accurate.
- Chinese or English OK; match the transcript language.`

	user := fmt.Sprintf(
		"Compress conversation messages seq %d-%d into the JSON schema.\n\nTRANSCRIPT:\n%s",
		startSeq, endSeq, transcript,
	)

	body := map[string]any{
		"model": profile.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0.2,
		"stream":      false,
		"max_tokens":  compactLLMMaxTokens,
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return CompactSummary{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), compactLLMTimeout)
	defer cancel()

	endpoint := openAICompatibleChatCompletionsURL(profile.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return CompactSummary{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(profile.APIKeySecret) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(profile.APIKeySecret))
	}

	client := &http.Client{Timeout: compactLLMTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return CompactSummary{}, err
	}
	defer resp.Body.Close()
	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return CompactSummary{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return CompactSummary{}, fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, truncateRunes(string(rawResp), 400))
	}

	content, err := extractChatCompletionText(rawResp)
	if err != nil {
		return CompactSummary{}, err
	}
	summary, err := parseCompactSummaryJSON(content)
	if err != nil {
		// Model returned prose — wrap as summary text rather than fail hard.
		return CompactSummary{
			Summary: strings.TrimSpace(content),
		}, nil
	}
	if strings.TrimSpace(summary.Summary) == "" {
		summary.Summary = fmt.Sprintf("Compacted session messages %d-%d via LLM.", startSeq, endSeq)
	}
	return summary, nil
}

type compactProvider struct {
	BaseURL      string
	Model        string
	APIKeySecret string
}

func (s SessionService) resolveCompactProvider(providerProfileID string) (compactProvider, error) {
	id := strings.TrimSpace(providerProfileID)
	if id != "" {
		row, err := s.repos.Providers.Get(id)
		if err != nil {
			return compactProvider{}, fmt.Errorf("provider profile %s: %w", id, err)
		}
		if !row.Active {
			return compactProvider{}, fmt.Errorf("provider profile %s is inactive", id)
		}
		return compactProvider{
			BaseURL:      row.BaseURL,
			Model:        firstNonEmptyString(row.Model, "default"),
			APIKeySecret: row.APIKeySecret,
		}, nil
	}

	// Prefer default active profile, then any active profile.
	rows, err := s.repos.Providers.List(100)
	if err != nil {
		return compactProvider{}, err
	}
	var fallback *model.ProviderProfile
	for i := range rows {
		row := rows[i]
		if !row.Active || strings.TrimSpace(row.BaseURL) == "" {
			continue
		}
		if row.IsDefault {
			return compactProvider{
				BaseURL:      row.BaseURL,
				Model:        firstNonEmptyString(row.Model, "default"),
				APIKeySecret: row.APIKeySecret,
			}, nil
		}
		if fallback == nil {
			fallback = &row
		}
	}
	if fallback != nil {
		return compactProvider{
			BaseURL:      fallback.BaseURL,
			Model:        firstNonEmptyString(fallback.Model, "default"),
			APIKeySecret: fallback.APIKeySecret,
		}, nil
	}
	return compactProvider{}, fmt.Errorf("no active provider profile configured for LLM compact")
}

func buildCompactTranscript(messages []model.Message, maxRunes int) string {
	var b strings.Builder
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		text := strings.TrimSpace(messageText(message))
		if text == "" {
			continue
		}
		// Soft-cap very long single messages (e.g. huge tool dumps pasted into chat).
		text = truncateRunes(text, 4000)
		line := fmt.Sprintf("[seq=%d role=%s]\n%s\n\n", message.Seq, message.Role, text)
		if utf8.RuneCountInString(b.String())+utf8.RuneCountInString(line) > maxRunes {
			b.WriteString("…[transcript truncated for model context]\n")
			break
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

func openAICompatibleChatCompletionsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

func extractChatCompletionText(raw []byte) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("provider response missing choices")
	}
	return contentToString(parsed.Choices[0].Message.Content), nil
}

func contentToString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := m["text"].(string); ok && strings.TrimSpace(t) != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(v)
	}
}

func parseCompactSummaryJSON(raw string) (CompactSummary, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CompactSummary{}, fmt.Errorf("empty model content")
	}
	// Strip optional ```json fences.
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```JSON")
		raw = strings.TrimPrefix(raw, "```")
		if idx := strings.LastIndex(raw, "```"); idx >= 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimSpace(raw)
	}
	// Locate outermost JSON object if prose wraps it.
	if !strings.HasPrefix(raw, "{") {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start >= 0 && end > start {
			raw = raw[start : end+1]
		}
	}
	var summary CompactSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return CompactSummary{}, err
	}
	return summary, nil
}

func truncateRunes(value string, max int) string {
	if max <= 0 || value == "" {
		return value
	}
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	if max < 4 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
