package service

import (
	"encoding/json"
	"strings"
)

// Option/map helpers shared by run start preparation.
func stringInput(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, _ := input[key].(string)
	return value
}

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func stringOption(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	switch value := options[key].(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func boolOption(options map[string]any, key string) bool {
	if options == nil {
		return false
	}
	value, _ := options[key].(bool)
	return value
}

func intOption(options map[string]any, key string) int {
	if options == nil {
		return 0
	}
	switch value := options[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return 0
}

func stringSliceOption(options map[string]any, key string) []string {
	if options == nil {
		return nil
	}
	raw, ok := options[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return value
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

func normalizedRuntimeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", "default", "auto":
		return defaultRuntimeMode
	case "per_run_process":
		return "per_run_process"
	case "single_core":
		return "single_core"
	default:
		return defaultRuntimeMode
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
