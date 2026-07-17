package service

import (
	"encoding/json"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/model"
	protows "redpanda/protocol/ws"
)

// broadcastSessionUpserted notifies all connected Desktops of a new/updated session.
// reason examples: user_create | schedule | fork
func broadcastSessionUpserted(hub *eventhub.Hub, session model.Session, extra map[string]any) {
	if hub == nil || session.ID == "" {
		return
	}
	payload := map[string]any{
		"session": sessionDTO(session),
		"reason":  stringFromExtra(extra, "reason", "upsert"),
	}
	for k, v := range extra {
		if k == "reason" {
			continue
		}
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	hub.Broadcast(protows.Envelope{
		Type:    protows.TypeEvent,
		Method:  protows.EventSessionUpserted,
		Payload: raw,
	})
}

// broadcastSessionDeleted notifies Desktops to drop a session from the list.
func broadcastSessionDeleted(hub *eventhub.Hub, sessionID, reason string) {
	if hub == nil || sessionID == "" {
		return
	}
	if reason == "" {
		reason = "user_delete"
	}
	raw, err := json.Marshal(map[string]any{
		"id":     sessionID,
		"reason": reason,
	})
	if err != nil {
		return
	}
	hub.Broadcast(protows.Envelope{
		Type:    protows.TypeEvent,
		Method:  protows.EventSessionDeleted,
		Payload: raw,
	})
}

func stringFromExtra(extra map[string]any, key, fallback string) string {
	if extra == nil {
		return fallback
	}
	if v, ok := extra[key].(string); ok && v != "" {
		return v
	}
	return fallback
}
