package provider

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type providerContractStub struct{}

func (providerContractStub) Name() string { return "contract_stub" }

func (providerContractStub) Complete(context.Context, ProviderRequest, func(ProviderChunk) error) error {
	return nil
}

var _ Provider = providerContractStub{}

func TestProviderProductionFilesDoNotImportRuntime(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), filepath.Clean(name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Decls {
			decl, ok := spec.(*ast.GenDecl)
			if !ok || decl.Tok != token.IMPORT {
				continue
			}
			for _, item := range decl.Specs {
				importSpec := item.(*ast.ImportSpec)
				path, err := strconv.Unquote(importSpec.Path.Value)
				if err != nil {
					t.Fatalf("unquote import in %s: %v", name, err)
				}
				if strings.HasSuffix(path, "/internal/runtime") || strings.Contains(path, "/internal/runtime/") {
					t.Errorf("production provider file %s imports runtime package %q", name, path)
				}
				if strings.HasSuffix(path, "/protocol/methods") {
					t.Errorf("production provider file %s imports broad wire DTO package %q", name, path)
				}
				if strings.HasSuffix(path, "/internal/tools") || strings.Contains(path, "/internal/tools/") {
					t.Errorf("production provider file %s imports agent tools package %q", name, path)
				}
			}
		}
	}
}

func TestProviderNamesRemainStable(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "echo", got: (EchoProvider{}).Name(), want: "echo"},
		{name: "OpenAI compatible", got: (HTTPCompatibleProvider{}).Name(), want: "openai_compatible"},
		{name: "OpenAI Responses", got: (OpenAIResponsesProvider{}).Name(), want: "openai_responses"},
		{name: "Anthropic", got: (AnthropicProvider{}).Name(), want: "anthropic"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("Name() = %q, want %q", test.got, test.want)
			}
		})
	}
}

func TestProviderFromOptionsCompatibility(t *testing.T) {
	disableStream := false
	tests := []struct {
		name           string
		options        RequestOptions
		fallbackStream bool
		wantOK         bool
		wantBaseURL    string
		wantAPIKey     string
		wantModel      string
	}{
		{
			name: "profile stream setting overrides fallback",
			options: RequestOptions{
				ProviderBaseURL: "https://provider.example/v1",
				Stream:          &disableStream,
			},
			fallbackStream: true,
			wantOK:         true,
			wantBaseURL:    "https://provider.example/v1",
			wantModel:      "default",
		},
		{
			name: "empty provider defaults to OpenAI compatible",
			options: RequestOptions{
				ProviderBaseURL: " https://provider.example/v1/ ",
				ProviderAPIKey:  " secret ",
				Model:           " model-a ",
			},
			fallbackStream: true,
			wantOK:         true,
			wantBaseURL:    "https://provider.example/v1",
			wantAPIKey:     "secret",
			wantModel:      "model-a",
		},
		{
			name: "legacy HTTP compatible alias remains accepted",
			options: RequestOptions{
				ProviderName:    " HTTP_COMPATIBLE ",
				ProviderBaseURL: "https://provider.example",
			},
			wantOK:      true,
			wantBaseURL: "https://provider.example",
			wantModel:   "default",
		},
		{
			name: "missing base URL disables override",
			options: RequestOptions{
				ProviderName: "openai_compatible",
				Model:        "ignored",
			},
			wantOK: false,
		},
		{
			name: "Anthropic provider is accepted",
			options: RequestOptions{
				ProviderName:    "anthropic",
				ProviderBaseURL: "https://provider.example",
			},
			wantOK:      true,
			wantBaseURL: "https://provider.example",
			wantModel:   "default",
		},
		{
			name: "OpenAI Responses provider is accepted",
			options: RequestOptions{
				ProviderName:    "openai_responses",
				ProviderBaseURL: "https://api.openai.com/v1",
			},
			wantOK:      true,
			wantBaseURL: "https://api.openai.com/v1",
			wantModel:   "default",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := Resolve(test.options, test.fallbackStream)
			if ok != test.wantOK {
				t.Fatalf("Resolve() ok = %v, want %v", ok, test.wantOK)
			}
			if !ok {
				return
			}
			config := configForProvider(t, got)
			if config.BaseURL != test.wantBaseURL || config.APIKey != test.wantAPIKey || config.Model != test.wantModel {
				t.Fatalf("provider override = %#v, want base=%q key=%q model=%q", got, test.wantBaseURL, test.wantAPIKey, test.wantModel)
			}
			wantStream := test.fallbackStream
			if test.options.Stream != nil {
				wantStream = *test.options.Stream
			}
			if config.Stream != wantStream {
				t.Fatalf("Stream = %v, want %v", config.Stream, wantStream)
			}
			if config.Client == nil || config.Client.Timeout != 0 {
				t.Fatalf("override client = %#v, want no response-body timeout", config.Client)
			}
			transport, ok := config.Client.Transport.(*http.Transport)
			if !ok || transport.ResponseHeaderTimeout != providerResponseHeaderTimeout {
				t.Fatalf("override transport = %#v, want %s response-header timeout", config.Client.Transport, providerResponseHeaderTimeout)
			}
		})
	}
}

func configForProvider(t *testing.T, value Provider) providerConfig {
	t.Helper()
	switch item := value.(type) {
	case HTTPCompatibleProvider:
		return item.config()
	case OpenAIResponsesProvider:
		return item.providerConfig
	case AnthropicProvider:
		return item.providerConfig
	default:
		t.Fatalf("unexpected provider type %T", value)
		return providerConfig{}
	}
}

func TestStreamAndNonStreamToolCallsNormalizeIdentically(t *testing.T) {
	nonStreamResponse := `{"choices":[{"message":{"tool_calls":[` +
		`{"id":"call_read","type":"function","function":{"name":"workspace__read_file","arguments":"{\"path\":\"README.md\"}"}},` +
		`{"id":"call_goal","type":"function","function":{"name":"goal__assess","arguments":"{\"status\":\"continue\"}"}}` +
		`]}}]}`
	streamResponse := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_read","function":{"name":"workspace__read_file","arguments":"{\"path\":\"README"}},{"index":1,"id":"call_goal","function":{"name":"goal__assess","arguments":"{\"status\":\""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":".md\"}"}},{"index":1,"function":{"arguments":"continue\"}"}}]}}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")

	nonStreamCalls := collectToolCalls(t, func(emit func(ProviderChunk) error) error {
		return completeHTTPResponse([]byte(nonStreamResponse), emit)
	})
	streamCalls := collectToolCalls(t, func(emit func(ProviderChunk) error) error {
		return (HTTPCompatibleProvider{}).completeStream(strings.NewReader(streamResponse), emit)
	})

	if !reflect.DeepEqual(streamCalls, nonStreamCalls) {
		t.Fatalf("normalized tool calls differ\nstream: %#v\nnon-stream: %#v", streamCalls, nonStreamCalls)
	}
}

func collectToolCalls(t *testing.T, complete func(func(ProviderChunk) error) error) []ProviderChunk {
	t.Helper()
	var chunks []ProviderChunk
	if err := complete(func(chunk ProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return chunks
}
