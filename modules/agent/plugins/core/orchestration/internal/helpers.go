package internal

// maxToolOutputBytes copied from agent/internal/tools/helpers.go.
const maxToolOutputBytes = 64 * 1024

type jsonNumber interface {
	Int64() (int64, error)
}

// ---------- argument helpers copied from agent/internal/tools/helpers.go ----------

func StringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func StringArgDefault(args map[string]any, key string, fallback string) string {
	imported := StringArg(args, key)
	return imported
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

func StringArgPresent(args map[string]any, key string) (string, bool) {
	value, ok := args[key].(string)
	return value, ok
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
