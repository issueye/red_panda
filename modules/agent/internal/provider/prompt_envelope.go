package provider

import "redpanda/protocol"

// PromptSchemaVersion is bumped whenever the byte-level prompt protocol changes.
// It is part of CacheEpoch so deployments invalidate provider caches explicitly.
const PromptSchemaVersion = protocol.PromptSchemaVersion

// PromptEnvelope is the only authoritative prompt representation in a Request.
// Segments are ordered from most stable to most dynamic; callers must append new
// tool transcript outside the envelope instead of recomposing earlier segments.
type PromptEnvelope struct {
	StablePrefix  []Message
	SessionPrefix []Message
	History       []Message
	TurnTail      []Message
	CacheEpoch    string
}

// FlattenMessages returns a detached, provider-ready view in protocol order.
func (p PromptEnvelope) FlattenMessages() []Message {
	total := len(p.StablePrefix) + len(p.SessionPrefix) + len(p.History) + len(p.TurnTail)
	messages := make([]Message, 0, total)
	messages = append(messages, p.StablePrefix...)
	messages = append(messages, p.SessionPrefix...)
	messages = append(messages, p.History...)
	messages = append(messages, p.TurnTail...)
	return messages
}

// Clone detaches all segment slices so a provider hook or loop turn cannot
// mutate the frozen prompt snapshot owned by the Run.
func (p PromptEnvelope) Clone() PromptEnvelope {
	p.StablePrefix = append([]Message(nil), p.StablePrefix...)
	p.SessionPrefix = append([]Message(nil), p.SessionPrefix...)
	p.History = append([]Message(nil), p.History...)
	p.TurnTail = append([]Message(nil), p.TurnTail...)
	return p
}
