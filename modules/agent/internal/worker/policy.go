package worker

import (
	"regexp"
	"strings"
)

const (
	// 为文件处理后的推理和最终分析报告预留的回合数。
	SummaryTurns = 8
	// 仅在未提供 max_turns、file_count 或 path 时使用的回退值。
	DefaultToolTurns = 16
)

// DelegatedDenylist is applied to Worker tool allowlists (canonical names only).
var DelegatedDenylist = []string{
	"worker.delegate",
	"worker.assignment.cancel",
	"worker.pool.status",
	"skill.run",
	"todo.write",
	"todo.list",
	"goal.create",
	"goal.plan",
	"goal.observe",
	"goal.assess",
	"goal.finish",
	"goal.list",
}

var workerNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// RecommendedTurns returns recommended turns = file count + analysis/summary reserve.
// No artificial upper bound; budget scales with directory size.
func RecommendedTurns(fileCount int) int {
	if fileCount < 1 {
		fileCount = 1
	}
	return fileCount + SummaryTurns
}

// EffectiveToolTurns determines tool turn budget for a delegated worker.
// 优先级：显式 max_turns > 基于 file_count 的公式 > 默认值。
// 没有硬性最大值，回合数会随待分析文件数量增长。
func EffectiveToolTurns(explicit int, fileCount int) int {
	if explicit > 0 {
		return explicit
	}
	if fileCount > 0 {
		return RecommendedTurns(fileCount)
	}
	return DefaultToolTurns
}

// isUsableFinalText 拒绝仅包含文本形式 <tool_call> 标记的提供方回退输出。
// 这些调用没有被执行，因此不构成最终答案或专业子代理报告。
func ReportUsable(report string) bool {
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

func SanitizeName(name string) string {
	cleaned := strings.Trim(workerNameSanitizer.ReplaceAllString(strings.TrimSpace(name), "-"), "-")
	if cleaned == "" {
		return "worker"
	}
	if len(cleaned) > 48 {
		return cleaned[:48]
	}
	return cleaned
}

func TruncateSummary(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "…"
}
