package service

import "encoding/json"

// marshalRuntimeToolJSON serializes Gateway-mediated tool output payloads.
// Domain helpers keep their field shapes; this only shares marshal + fallback.
func marshalRuntimeToolJSON(payload map[string]any, fallback string) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fallback
	}
	return string(raw)
}
