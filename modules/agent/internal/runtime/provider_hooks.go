package runtime

import (
	"context"
	"fmt"
	"strings"

	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/runtime/hooks"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func (r *Runtime) completeProvider(ctx context.Context, params methods.ReplyParams, request provider.Request, emit func(provider.ProviderChunk) error) error {
	hookCtx := runtimeHookContext(ctx, params)
	contextOutcome := r.bus.Emit(hookCtx, hooks.HookContextCompose, providerRequestHookEvent(r.provider.Name(), request))
	if contextOutcome.Blocked || contextOutcome.Cancelled {
		return fmt.Errorf("context composition blocked: %s", hookOutcomeReason(contextOutcome, "blocked by hook"))
	}
	if err := applyProviderRequestHookEvent(&request, contextOutcome.Event); err != nil {
		return fmt.Errorf("context composition hook: %w", err)
	}

	outcome := r.bus.Emit(hookCtx, hooks.HookBeforeProviderRequest, providerRequestHookEvent(r.provider.Name(), request))
	if outcome.Blocked || outcome.Cancelled {
		return fmt.Errorf("provider request blocked: %s", hookOutcomeReason(outcome, "blocked by hook"))
	}
	if err := applyProviderRequestHookEvent(&request, outcome.Event); err != nil {
		return fmt.Errorf("provider request hook: %w", err)
	}

	var textBytes, toolCalls, chunks int
	var usage provider.ProviderUsage
	err := r.provider.Complete(ctx, request, func(chunk provider.ProviderChunk) error {
		chunks++
		textBytes += len(chunk.Delta)
		toolCalls += len(chunk.ToolCalls)
		if chunk.Usage != nil {
			usage = mergeProviderUsage(usage, *chunk.Usage)
			chunk.Usage = nil
		}
		return emit(chunk)
	})

	afterCtx := ctx
	if ctx.Err() != nil {
		afterCtx = context.WithoutCancel(ctx)
	}
	event := map[string]any{
		"provider_name":   r.provider.Name(),
		"model":           request.Options.Model,
		"chunk_count":     chunks,
		"text_bytes":      textBytes,
		"tool_call_count": toolCalls,
		"success":         err == nil,
	}
	if usage != (provider.ProviderUsage{}) {
		diagnostic := r.diagnoseCacheCall(request, usage, err == nil)
		usagePayload := cacheUsagePayload(r.provider.Name(), request, usage, diagnostic)
		_ = r.emitEvent(afterCtx, params, events.EventUsage, nil, usagePayload)
		for key, value := range usagePayload {
			event[key] = value
		}
	}
	if err != nil {
		event["error"] = err.Error()
	}
	r.bus.Emit(runtimeHookContext(afterCtx, params), hooks.HookAfterProviderResponse, event)
	return err
}

func mergeProviderUsage(current, next provider.ProviderUsage) provider.ProviderUsage {
	if next.InputTokens > current.InputTokens {
		current.InputTokens = next.InputTokens
	}
	if next.OutputTokens > current.OutputTokens {
		current.OutputTokens = next.OutputTokens
	}
	if next.CacheReadTokens > current.CacheReadTokens {
		current.CacheReadTokens = next.CacheReadTokens
	}
	if next.CacheWriteTokens > current.CacheWriteTokens {
		current.CacheWriteTokens = next.CacheWriteTokens
	}
	return current
}

func cacheUsagePayload(providerName string, request provider.Request, usage provider.ProviderUsage, diagnostic cacheCallDiagnostic) map[string]any {
	payload := map[string]any{
		"provider_name":      providerName,
		"model":              request.Options.Model,
		"input_tokens":       usage.InputTokens,
		"output_tokens":      usage.OutputTokens,
		"cache_read_tokens":  usage.CacheReadTokens,
		"cache_write_tokens": usage.CacheWriteTokens,
		"cache_hit":          diagnostic.cacheHit,
		"cache_hit_ratio":    diagnostic.hitRatio,
		"cache_mode":         diagnostic.cacheMode,
		"cache_active":       diagnostic.cacheActive,
		"cache_epoch":        request.Prompt.CacheEpoch,
		"prefix_hash":        diagnostic.prefixHash,
	}
	if diagnostic.missReason != "" {
		payload["cache_miss_reason"] = diagnostic.missReason
	}
	return payload
}

func providerRequestHookEvent(providerName string, request provider.Request) map[string]any {
	promptSegment := func(source []provider.Message) []any {
		messages := make([]any, 0, len(source))
		for _, message := range source {
			messages = append(messages, map[string]any{
				"role":    message.Role,
				"content": providerContentToHook(message.Content),
			})
		}
		return messages
	}
	prompt := map[string]any{
		"stable_prefix":  promptSegment(request.Prompt.StablePrefix),
		"session_prefix": promptSegment(request.Prompt.SessionPrefix),
		"history":        promptSegment(request.Prompt.History),
		"turn_tail":      promptSegment(request.Prompt.TurnTail),
		"cache_epoch":    request.Prompt.CacheEpoch,
	}
	definitions := make([]any, 0, len(request.Tools))
	for _, definition := range request.Tools {
		definitions = append(definitions, map[string]any{
			"name":         definition.Name,
			"display_name": definition.DisplayName,
			"description":  definition.Description,
			"risk":         string(definition.Risk),
			"parameters":   definition.Parameters,
		})
	}
	headers := make(map[string]any, len(request.Options.Headers))
	for name, value := range provider.SanitizeHeaders(request.Options.Headers) {
		headers[name] = value
	}
	return map[string]any{
		"provider_name": providerName,
		"model":         request.Options.Model,
		"prompt":        prompt,
		"tools":         definitions,
		"headers":       headers,
		"cache_policy": map[string]any{
			"mode":                effectiveDiagnosticCacheMode(request.Options, providerName),
			"cache_key_supported": request.Options.CacheKeySupported,
			"retention":           request.Options.CacheRetention,
			"min_cache_tokens":    request.Options.MinCacheTokens,
		},
	}
}

func providerContentToHook(content any) any {
	parts, ok := content.([]provider.Part)
	if !ok {
		return content
	}
	result := make([]any, 0, len(parts))
	for _, part := range parts {
		item := map[string]any{"type": part.Type, "text": part.Text}
		if part.ImageURL != nil {
			item["image_url"] = map[string]any{"url": part.ImageURL.URL}
		}
		if part.Source != nil {
			item["source"] = map[string]any{"type": part.Source.Type, "media_type": part.Source.MediaType, "data": part.Source.Data}
		}
		result = append(result, item)
	}
	return result
}

func applyProviderRequestHookEvent(request *provider.Request, event map[string]any) error {
	if raw, exists := event["prompt"]; exists {
		prompt, err := providerPromptFromHook(raw)
		if err != nil {
			return err
		}
		request.Prompt = prompt
	}
	if raw, exists := event["tools"]; exists {
		definitions, err := providerToolsFromHook(raw)
		if err != nil {
			return err
		}
		request.Tools = definitions
	}
	if raw, exists := event["headers"]; exists {
		headers, err := providerHeadersFromHook(raw)
		if err != nil {
			return err
		}
		request.Options.Headers = provider.SanitizeHeaders(headers)
	}
	request.Tools = provider.CanonicalToolDefinitions(request.Tools)
	request.Prompt.CacheEpoch = provider.ComputeCacheEpoch(request.Options.ProviderName, request.Options.Model, request.Tools, request.Prompt)
	return nil
}

func providerPromptFromHook(raw any) (provider.PromptEnvelope, error) {
	item, ok := raw.(map[string]any)
	if !ok {
		return provider.PromptEnvelope{}, fmt.Errorf("prompt must be an object")
	}
	readSegment := func(name string) ([]provider.Message, error) {
		rawSegment, exists := item[name]
		if !exists {
			return nil, nil
		}
		messages, err := providerMessagesFromHook(rawSegment)
		if err != nil {
			return nil, fmt.Errorf("prompt.%s: %w", name, err)
		}
		return messages, nil
	}
	stable, err := readSegment("stable_prefix")
	if err != nil {
		return provider.PromptEnvelope{}, err
	}
	session, err := readSegment("session_prefix")
	if err != nil {
		return provider.PromptEnvelope{}, err
	}
	history, err := readSegment("history")
	if err != nil {
		return provider.PromptEnvelope{}, err
	}
	tail, err := readSegment("turn_tail")
	if err != nil {
		return provider.PromptEnvelope{}, err
	}
	return provider.PromptEnvelope{
		StablePrefix: stable, SessionPrefix: session, History: history, TurnTail: tail,
	}, nil
}

func providerMessagesFromHook(raw any) ([]provider.Message, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("messages must be an array")
	}
	result := make([]provider.Message, 0, len(items))
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("messages[%d] must be an object", index)
		}
		role, ok := item["role"].(string)
		if !ok || strings.TrimSpace(role) == "" {
			return nil, fmt.Errorf("messages[%d].role is required", index)
		}
		content, err := providerContentFromHook(item["content"])
		if err != nil {
			return nil, fmt.Errorf("messages[%d].content: %w", index, err)
		}
		result = append(result, provider.Message{Role: role, Content: content})
	}
	return result, nil
}

func providerContentFromHook(raw any) (any, error) {
	if text, ok := raw.(string); ok {
		return text, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("must be text or a part array")
	}
	parts := make([]provider.Part, 0, len(items))
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("part %d must be an object", index)
		}
		part := provider.Part{Type: hookString(item["type"]), Text: hookString(item["text"])}
		if rawImage, ok := item["image_url"].(map[string]any); ok {
			part.ImageURL = &provider.ImageURL{URL: hookString(rawImage["url"])}
		}
		if rawSource, ok := item["source"].(map[string]any); ok {
			part.Source = &provider.ImageSource{Type: hookString(rawSource["type"]), MediaType: hookString(rawSource["media_type"]), Data: hookString(rawSource["data"])}
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func providerToolsFromHook(raw any) ([]tools.Definition, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("tools must be an array")
	}
	result := make([]tools.Definition, 0, len(items))
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tools[%d] must be an object", index)
		}
		name := hookString(item["name"])
		if name == "" {
			return nil, fmt.Errorf("tools[%d].name is required", index)
		}
		parameters, _ := item["parameters"].(map[string]any)
		result = append(result, tools.Definition{
			Name: name, DisplayName: hookString(item["display_name"]), Description: hookString(item["description"]),
			Risk: tools.Risk(hookString(item["risk"])), Parameters: parameters,
		})
	}
	return result, nil
}

func providerHeadersFromHook(raw any) (map[string]string, error) {
	items, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("headers must be an object")
	}
	result := make(map[string]string, len(items))
	for name, rawValue := range items {
		value, ok := rawValue.(string)
		if !ok {
			return nil, fmt.Errorf("header %q must be a string", name)
		}
		result[name] = value
	}
	return result, nil
}

func hookString(value any) string {
	text, _ := value.(string)
	return text
}
