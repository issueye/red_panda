package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"redpanda/agent/internal/provider"
)

const maxCacheDiagnosticSessions = 2048

type cacheDiagnosticState struct {
	epoch string
	units []string
}

type cacheCallDiagnostic struct {
	cacheMode   string
	missReason  string
	prefixHash  string
	hitRatio    float64
	cacheHit    bool
	cacheActive bool
}

func (r *Runtime) diagnoseCacheCall(request provider.Request, usage provider.ProviderUsage, success bool) cacheCallDiagnostic {
	mode := effectiveDiagnosticCacheMode(request.Options, r.provider.Name())
	units := cachePrefixUnits(request)
	diagnostic := cacheCallDiagnostic{
		cacheMode:   mode,
		prefixHash:  hashCacheUnits(units),
		cacheHit:    usage.CacheReadTokens > 0,
		cacheActive: mode != provider.CacheModeDisabled,
	}
	if usage.InputTokens > 0 {
		diagnostic.hitRatio = float64(usage.CacheReadTokens) / float64(usage.InputTokens)
		if diagnostic.hitRatio > 1 {
			diagnostic.hitRatio = 1
		}
	}

	key := cacheDiagnosticKey(request, r.provider.Name())
	r.cacheDiagnosticsMu.Lock()
	defer r.cacheDiagnosticsMu.Unlock()
	previous, hasPrevious := r.cacheDiagnostics[key]
	if success {
		if r.cacheDiagnostics == nil || len(r.cacheDiagnostics) >= maxCacheDiagnosticSessions {
			r.cacheDiagnostics = make(map[string]cacheDiagnosticState)
		}
		r.cacheDiagnostics[key] = cacheDiagnosticState{
			epoch: request.Prompt.CacheEpoch,
			units: append([]string(nil), units...),
		}
	}

	if diagnostic.cacheHit {
		return diagnostic
	}
	switch {
	case !diagnostic.cacheActive:
		diagnostic.missReason = "unsupported"
	case request.Options.MinCacheTokens > 0 && usage.InputTokens > 0 && usage.InputTokens < request.Options.MinCacheTokens:
		diagnostic.missReason = "below_minimum"
	case hasPrevious && previous.epoch != request.Prompt.CacheEpoch:
		diagnostic.missReason = "epoch_changed"
	case hasPrevious && !cacheUnitsArePrefix(previous.units, units):
		diagnostic.missReason = "prefix_changed"
	case hasPrevious && usage.InputTokens > 0:
		diagnostic.missReason = "provider_miss"
	default:
		diagnostic.missReason = "unknown"
	}
	return diagnostic
}

func effectiveDiagnosticCacheMode(options provider.RequestOptions, providerName string) string {
	if mode := strings.ToLower(strings.TrimSpace(options.CacheMode)); mode != "" {
		return mode
	}
	name := strings.ToLower(strings.TrimSpace(options.ProviderName))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(providerName))
	}
	if strings.Contains(name, "anthropic") {
		return provider.CacheModeExplicit
	}
	return provider.CacheModeImplicit
}

func cacheDiagnosticKey(request provider.Request, providerName string) string {
	sessionID := strings.TrimSpace(request.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(request.RunID)
	}
	return strings.Join([]string{sessionID, providerName, request.Options.ProviderName, request.Options.Model}, "\x00")
}

func cachePrefixUnits(request provider.Request) []string {
	units := make([]string, 0, 1+len(request.Prompt.FlattenMessages())+len(request.ToolRounds))
	units = append(units, hashCacheUnit(request.Tools))
	for _, message := range request.Prompt.FlattenMessages() {
		units = append(units, hashCacheUnit(message))
	}
	for _, round := range diagnosticToolRounds(request) {
		units = append(units, hashCacheUnit(round))
	}
	return units
}

func diagnosticToolRounds(request provider.Request) [][]provider.ToolExchange {
	if len(request.ToolRounds) > 0 {
		return request.ToolRounds
	}
	rounds := make([][]provider.ToolExchange, 0, len(request.ToolHistory))
	for _, exchange := range request.ToolHistory {
		rounds = append(rounds, []provider.ToolExchange{exchange})
	}
	return rounds
}

func hashCacheUnit(value any) string {
	raw, err := provider.CanonicalJSON(value)
	if err != nil {
		raw = []byte(fmt.Sprintf("unsupported:%T", value))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func hashCacheUnits(units []string) string {
	sum := sha256.Sum256([]byte(strings.Join(units, "\n")))
	return hex.EncodeToString(sum[:])
}

func cacheUnitsArePrefix(previous, current []string) bool {
	if len(previous) > len(current) {
		return false
	}
	for index := range previous {
		if previous[index] != current[index] {
			return false
		}
	}
	return true
}
