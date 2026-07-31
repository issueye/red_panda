package provider

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"redpanda/protocol/tools"
)

type streamToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func (p HTTPCompatibleProvider) completeStream(reader io.Reader, emit func(ProviderChunk) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	toolCalls := map[int]*streamToolCall{}
	var emittedContent bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Usage struct {
				CachedTokens     int `json:"cached_tokens"`
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				PromptDetails    struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return err
		}
		cachedTokens := chunk.Usage.PromptDetails.CachedTokens
		if chunk.Usage.CachedTokens > cachedTokens {
			cachedTokens = chunk.Usage.CachedTokens
		}
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 || cachedTokens > 0 {
			if err := emit(ProviderChunk{Usage: &ProviderUsage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens, CacheReadTokens: cachedTokens}}); err != nil {
				return err
			}
		}
		for _, choice := range chunk.Choices {
			reasoning := choice.Delta.ReasoningContent
			if reasoning == "" {
				reasoning = choice.Delta.Reasoning
			}
			if reasoning != "" {
				if err := emit(ProviderChunk{ReasoningDelta: reasoning}); err != nil {
					return err
				}
			}
			if choice.Delta.Content != "" {
				emittedContent = true
				if err := emit(ProviderChunk{Delta: choice.Delta.Content}); err != nil {
					return err
				}
			}
			for _, deltaCall := range choice.Delta.ToolCalls {
				call := toolCalls[deltaCall.Index]
				if call == nil {
					call = &streamToolCall{}
					toolCalls[deltaCall.Index] = call
				}
				if deltaCall.ID != "" {
					call.ID = deltaCall.ID
				}
				if deltaCall.Function.Name != "" {
					call.Name = internalToolName(deltaCall.Function.Name)
				}
				if deltaCall.Function.Arguments != "" {
					call.Arguments.WriteString(deltaCall.Function.Arguments)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(toolCalls) > 0 {
		calls := make([]tools.Call, 0, len(toolCalls))
		for index := 0; index < len(toolCalls); index++ {
			call := toolCalls[index]
			if call == nil || call.Name == "" {
				continue
			}
			var args map[string]any
			if text := call.Arguments.String(); text != "" {
				_ = json.Unmarshal([]byte(text), &args)
			}
			calls = append(calls, tools.Call{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: args,
			})
		}
		if len(calls) > 0 {
			return emit(ProviderChunk{ToolCalls: calls})
		}
	}
	if emittedContent {
		return emit(ProviderChunk{Final: true})
	}
	return emit(ProviderChunk{Final: true})
}
