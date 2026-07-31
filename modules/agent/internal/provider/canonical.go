package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"redpanda/protocol/tools"
)

// CanonicalToolDefinitions returns a detached copy ordered by the public name
// actually sent to providers. Nested schema maps are copied recursively.
func CanonicalToolDefinitions(definitions []tools.Definition) []tools.Definition {
	items := make([]tools.Definition, len(definitions))
	for index, definition := range definitions {
		items[index] = definition
		items[index].Parameters = canonicalStringMap(definition.Parameters)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := publicToolName(items[i].Name), publicToolName(items[j].Name)
		if left == right {
			return items[i].Name < items[j].Name
		}
		return left < right
	})
	return items
}

// CanonicalJSON serializes supported prompt values with deterministic object
// key order. encoding/json sorts string map keys; canonicalValue also detaches
// nested maps and slices so later caller mutations cannot change hashed input.
func CanonicalJSON(value any) ([]byte, error) {
	return json.Marshal(canonicalValue(value))
}

// ComputeCacheEpoch creates an opaque invalidation identity. It intentionally
// excludes History, TurnTail, credentials, Todo state, time, and tool results.
func ComputeCacheEpoch(providerName, model string, definitions []tools.Definition, prompt PromptEnvelope) string {
	payload := struct {
		SchemaVersion string             `json:"prompt_schema_version"`
		Provider      string             `json:"provider"`
		Model         string             `json:"model"`
		Tools         []tools.Definition `json:"tools"`
		StablePrefix  []Message          `json:"stable_prefix"`
		SessionPrefix []Message          `json:"session_prefix"`
	}{
		SchemaVersion: PromptSchemaVersion,
		Provider:      providerName, Model: model,
		Tools:         CanonicalToolDefinitions(definitions),
		StablePrefix:  append([]Message(nil), prompt.StablePrefix...),
		SessionPrefix: append([]Message(nil), prompt.SessionPrefix...),
	}
	raw, err := CanonicalJSON(payload)
	if err != nil {
		// Known prompt values are JSON-safe. Retain a deterministic marker if a
		// future extension violates that invariant instead of hashing raw content.
		raw = []byte("prompt-cache-epoch:unsupported-value")
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ComputeScopedCacheKey isolates provider cache routing per conversation while
// retaining the same key across continued runs in that session.
func ComputeScopedCacheKey(sessionID, cacheEpoch string) string {
	epoch := strings.TrimSpace(cacheEpoch)
	if epoch == "" {
		return ""
	}
	scope := strings.TrimSpace(sessionID)
	if scope == "" {
		return epoch
	}
	sum := sha256.Sum256([]byte(scope + "\x00" + epoch))
	return hex.EncodeToString(sum[:])
}

func canonicalStringMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = canonicalValue(value)
	}
	return result
}

func canonicalValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return canonicalStringMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = canonicalValue(item)
		}
		return result
	case []map[string]any:
		result := make([]map[string]any, len(typed))
		for index, item := range typed {
			result[index] = canonicalStringMap(item)
		}
		return result
	default:
		return value
	}
}
