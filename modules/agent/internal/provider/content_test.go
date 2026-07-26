package provider

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAICompatibleContentTextOnlyStaysString(t *testing.T) {
	if got := openAICompatibleContent("hello"); got != "hello" {
		t.Fatalf("got %#v", got)
	}
	if got := openAICompatibleContent([]Part{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}); got != "a\nb" {
		t.Fatalf("pure text parts should collapse to string, got %#v", got)
	}
}

func TestOpenAICompatibleContentImageParts(t *testing.T) {
	parts := []Part{
		{Type: "text", Text: "describe"},
		{Type: "image_url", ImageURL: &ImageURL{URL: "data:image/png;base64,aaa"}},
	}
	got := openAICompatibleContent(parts)
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"type":"text","text":"describe"},{"image_url":{"url":"data:image/png;base64,aaa"},"type":"image_url"}]`
	// order of keys in maps is stable enough via Marshal of []map constructed in code
	var gotV, wantV any
	_ = json.Unmarshal(raw, &gotV)
	_ = json.Unmarshal([]byte(want), &wantV)
	if !reflect.DeepEqual(gotV, wantV) {
		t.Fatalf("got %s want %s", raw, want)
	}
}

func TestAnthropicContentImageSource(t *testing.T) {
	parts := []Part{
		{Type: "text", Text: "look"},
		{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "abc"}},
	}
	got := anthropicMessageContent(parts)
	arr, ok := got.([]map[string]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("got %#v", got)
	}
	if arr[0]["type"] != "text" || arr[0]["text"] != "look" {
		t.Fatalf("text part = %#v", arr[0])
	}
	src := arr[1]["source"].(map[string]any)
	if arr[1]["type"] != "image" || src["media_type"] != "image/png" || src["data"] != "abc" {
		t.Fatalf("image part = %#v", arr[1])
	}
}

func TestOpenAIResponsesContentInputImage(t *testing.T) {
	parts := []Part{
		{Type: "text", Text: "hi"},
		{Type: "image_url", ImageURL: &ImageURL{URL: "data:image/jpeg;base64,xyz"}},
	}
	got := openAIResponsesContent(parts)
	arr, ok := got.([]map[string]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("got %#v", got)
	}
	if arr[0]["type"] != "input_text" || arr[1]["type"] != "input_image" {
		t.Fatalf("parts = %#v", arr)
	}
	if arr[1]["image_url"] != "data:image/jpeg;base64,xyz" {
		t.Fatalf("image_url = %#v", arr[1]["image_url"])
	}
}

func TestOpenAICompatibleMessagesWithImageParts(t *testing.T) {
	req := ProviderRequest{
		Messages: []Message{
			{Role: "user", Content: []Part{
				{Type: "text", Text: "what is this?"},
				{Type: "image_url", ImageURL: &ImageURL{URL: "data:image/png;base64,QQ=="}},
			}},
		},
	}
	msgs := openAICompatibleMessages(req)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %#v", msgs)
	}
	content, ok := msgs[0]["content"].([]map[string]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v", msgs[0]["content"])
	}
	if content[1]["type"] != "image_url" {
		t.Fatalf("expected image_url part, got %#v", content[1])
	}
}

func TestEchoProviderAttachmentReceipt(t *testing.T) {
	var chunks []ProviderChunk
	err := (EchoProvider{}).Complete(t.Context(), ProviderRequest{
		Input: "describe",
		Attachments: []RequestAttachment{{
			ID: "att_1", MIME: "image/png", ByteSize: 182233, DataB64: "AAAA",
		}},
	}, func(chunk ProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 1 {
		t.Fatal("no chunks")
	}
	if !strings.Contains(chunks[0].Delta, "Received 1 image(s): att_1 (image/png, 182233 bytes)") {
		t.Fatalf("delta = %q", chunks[0].Delta)
	}
}

func TestRedactImagePayloadsStripsDataURL(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + strings.Repeat("A", 80) + `"}}]}]}`)
	out := redactImagePayloads(body)
	if strings.Contains(string(out), strings.Repeat("A", 80)) {
		t.Fatalf("base64 leaked: %s", out)
	}
	if !strings.Contains(string(out), "redacted base64") {
		t.Fatalf("expected redaction marker: %s", out)
	}
}
