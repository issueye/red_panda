package provider

import (
	"context"
	"net/http"
	"time"
)

type providerConfig struct {
	BaseURL        string
	APIKey         string
	Model          string
	Stream         bool
	Client         *http.Client
	MaxAttempts    int
	RetryBaseDelay time.Duration
}

func (p HTTPCompatibleProvider) config() providerConfig {
	c := p.providerConfig
	if p.BaseURL != "" {
		c.BaseURL = p.BaseURL
	}
	if p.APIKey != "" {
		c.APIKey = p.APIKey
	}
	if p.Model != "" {
		c.Model = p.Model
	}
	if p.Client != nil {
		c.Client = p.Client
	}
	if p.MaxAttempts != 0 {
		c.MaxAttempts = p.MaxAttempts
	}
	if p.RetryBaseDelay != 0 {
		c.RetryBaseDelay = p.RetryBaseDelay
	}
	c.Stream = p.Stream || c.Stream
	return c
}

func completeResolvedProvider(ctx context.Context, p Provider, req Request, emit func(ProviderChunk) error) error {
	switch resolved := p.(type) {
	case HTTPCompatibleProvider:
		return resolved.complete(ctx, req, emit)
	case OpenAIResponsesProvider:
		return resolved.complete(ctx, req, emit)
	case AnthropicProvider:
		return resolved.complete(ctx, req, emit)
	default:
		return resolved.Complete(ctx, req, emit)
	}
}

type providerAttempt func(context.Context, Request, func(ProviderChunk) error) error

func completeWithRetry(ctx context.Context, config providerConfig, req Request, emit func(ProviderChunk) error, attempt providerAttempt) error {
	maxAttempts := config.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultProviderMaxAttempts
	}
	delay := config.RetryBaseDelay
	if delay <= 0 {
		delay = defaultProviderRetryBaseDelay
	}
	for index := 0; index < maxAttempts; index++ {
		emitted := false
		err := attempt(ctx, req, func(chunk ProviderChunk) error {
			emitted = true
			return emit(chunk)
		})
		if err == nil {
			return nil
		}
		if emitted || index == maxAttempts-1 || !isRetryableProviderError(ctx, err) {
			return err
		}
		if err := waitProviderRetry(ctx, delay*time.Duration(1<<index)); err != nil {
			return err
		}
	}
	return nil
}
