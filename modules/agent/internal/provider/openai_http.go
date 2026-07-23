package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"redpanda/protocol/tools"
)

const (
	defaultProviderMaxAttempts    = 3
	defaultProviderRetryBaseDelay = 500 * time.Millisecond
)

type HTTPCompatibleProvider struct {
	providerConfig
	// Exported fields preserve direct construction for tests and embedded clients.
	BaseURL        string
	APIKey         string
	Model          string
	Stream         bool
	Client         *http.Client
	MaxAttempts    int
	RetryBaseDelay time.Duration
}

func (p HTTPCompatibleProvider) Name() string {
	return "openai_compatible"
}

func (p HTTPCompatibleProvider) Complete(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	if override, ok := Resolve(req.Options, p.Stream); ok {
		return completeResolvedProvider(ctx, override, req, emit)
	}
	return p.complete(ctx, req, emit)
}

func (p HTTPCompatibleProvider) complete(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	return completeWithRetry(ctx, p.config(), req, emit, p.completeAttempt)
}

func (p HTTPCompatibleProvider) completeAttempt(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	config := p.config()
	model := req.Options.Model
	if model == "" {
		model = config.Model
	}
	body := map[string]any{
		"model":    model,
		"messages": openAICompatibleMessages(req),
		"stream":   config.Stream,
	}
	if effort := strings.TrimSpace(req.Options.ReasoningEffort); effort != "" {
		body["reasoning_effort"] = effort
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAICompatibleTools(req.Tools)
		body["tool_choice"] = "auto"
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := openAICompatibleChatCompletionsURL(config.BaseURL)
	if path, logErr := logLLMRequest(req.Options.LogLLMRequests, req.RunID, req.SessionID, model, endpoint, rawBody); logErr != nil {
		// 诊断失败不能导致面向用户的请求失败。
		fmt.Fprintf(os.Stderr, "red-panda-agent: llm request log failed: %v\n", logErr)
	} else if path != "" {
		fmt.Fprintf(os.Stderr, "red-panda-agent: llm request logged to %s\n", path)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+config.APIKey)
	}
	resp, err := config.Client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		rawResp, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return providerHTTPError{StatusCode: resp.StatusCode, Body: string(rawResp)}
	}
	if config.Stream {
		return p.completeStream(resp.Body, emit)
	}
	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return completeHTTPResponse(rawResp, emit)
}

type providerHTTPError struct {
	StatusCode int
	Body       string
}

func (e providerHTTPError) Error() string {
	return fmt.Sprintf("provider returned HTTP %d: %s", e.StatusCode, e.Body)
}

func isRetryableProviderError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var httpErr providerHTTPError
	if errors.As(err, &httpErr) {
		// 401: credential/token may be briefly unavailable (gateway proxy, plugin
		// token refresh). Still stop after MaxAttempts like other retryable codes.
		return httpErr.StatusCode == http.StatusUnauthorized ||
			httpErr.StatusCode == http.StatusRequestTimeout ||
			httpErr.StatusCode == http.StatusTooManyRequests ||
			httpErr.StatusCode >= http.StatusInternalServerError
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func waitProviderRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func openAICompatibleChatCompletionsURL(raw string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

func completeHTTPResponse(rawResp []byte, emit func(ProviderChunk) error) error {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawResp, &parsed); err != nil {
		return err
	}
	if len(parsed.Choices) == 0 {
		return fmt.Errorf("provider returned no choices")
	}
	message := parsed.Choices[0].Message
	if len(message.ToolCalls) > 0 {
		calls := make([]tools.Call, 0, len(message.ToolCalls))
		for _, toolCall := range message.ToolCalls {
			var args map[string]any
			if toolCall.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
			}
			calls = append(calls, tools.Call{
				ID:        toolCall.ID,
				Name:      internalToolName(toolCall.Function.Name),
				Arguments: args,
			})
		}
		return emit(ProviderChunk{ToolCalls: calls})
	}
	if err := emit(ProviderChunk{Delta: message.Content}); err != nil {
		return err
	}
	return emit(ProviderChunk{Final: true})
}
