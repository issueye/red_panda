package tools

import "strings"

const maxToolOutputBytes = 64 * 1024

// CanonicalToolName maps legacy aliases to the stable tool name used in
// schemas and dispatch (docs/47 Wave B). Call once at the runner entry so
// policy and switch cases can converge over time without breaking callers.
func CanonicalToolName(name string) string {
	switch strings.TrimSpace(name) {
	case "todo_write":
		return "todo.write"
	default:
		return strings.TrimSpace(name)
	}
}

func TruncateToolOutput(value string) string {
	if len(value) <= maxToolOutputBytes {
		return value
	}
	return value[:maxToolOutputBytes] + "\n[truncated]"
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func StringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func StringArgDefault(args map[string]any, key string, fallback string) string {
	value := strings.TrimSpace(StringArg(args, key))
	if value == "" {
		return fallback
	}
	return value
}

func IntArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case jsonNumber:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func BoolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func stringArgPresent(args map[string]any, key string) (string, bool) {
	value, ok := args[key].(string)
	return value, ok
}

type jsonNumber interface {
	Int64() (int64, error)
}
