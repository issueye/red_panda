package runtime

import (
	"regexp"
	"strings"
)

const (
	// Turns reserved for reasoning + writing the final analysis report after file work.
	subagentSummaryTurns = 8
	// Fallback only when neither max_turns nor file_count/path is provided.
	defaultSubagentToolTurns = 16
)

var subagentRunDenylist = []string{
	"subagent.run",
	"subagent.list",
	"subagent.cancel",
	"subagent.reset",
	"subagent.pool_status",
	"subagent.pool_resize",
	"subagent.pool_reset",
	"skill.run",
	"todo.write",
	"todo.list",
	"todo_write",
	"goal.write",
	"goal.update",
	"goal.checkpoint",
	"goal.complete",
	"goal.list",
}

var subagentNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// recommendedSubagentTurns is file_count + summary/analysis allowance.
// There is no artificial maximum; budget scales with the directory size.
func recommendedSubagentTurns(fileCount int) int {
	if fileCount < 1 {
		fileCount = 1
	}
	return fileCount + subagentSummaryTurns
}

// effectiveSubagentToolTurns picks the specialist budget.
// Priority: explicit max_turns > file_count formula > default.
// No hard maximum cap: turns scale with files being analyzed.
func effectiveSubagentToolTurns(explicit int, fileCount int) int {
	if explicit > 0 {
		return explicit
	}
	if fileCount > 0 {
		return recommendedSubagentTurns(fileCount)
	}
	return defaultSubagentToolTurns
}

// isUsableFinalText rejects provider fallback output that contains only
// textual <tool_call> markup. Those calls were not executed and are not a
// final answer or specialist report.
func isUsableFinalText(report string) bool {
	text := strings.TrimSpace(report)
	if text == "" {
		return false
	}
	if !strings.Contains(strings.ToLower(text), "<tool_call") {
		return true
	}

	for {
		start := strings.Index(strings.ToLower(text), "<tool_call")
		if start < 0 {
			break
		}
		rest := text[start:]
		end := strings.Index(strings.ToLower(rest), "</tool_call")
		if end < 0 {
			text = text[:start]
			break
		}
		closeEnd := strings.Index(rest[end:], ">")
		if closeEnd < 0 {
			text = text[:start]
			break
		}
		text = text[:start] + text[start+end+closeEnd+1:]
	}
	return strings.TrimSpace(text) != ""
}

func sanitizeSubagentName(name string) string {
	cleaned := strings.Trim(subagentNameSanitizer.ReplaceAllString(strings.TrimSpace(name), "-"), "-")
	if cleaned == "" {
		return "worker"
	}
	if len(cleaned) > 48 {
		return cleaned[:48]
	}
	return cleaned
}

func truncateSummary(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "…"
}
