package service

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
	protows "redpanda/protocol/ws"
)

func decodeContent(t *testing.T, contentJSON string) []methods.ContentBlock {
	t.Helper()
	var content []methods.ContentBlock
	if err := json.Unmarshal([]byte(contentJSON), &content); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	return content
}

// newRunServiceWithAttachments builds a RunService with the AttachmentService
// wired (production path goes through Set; tests need it for path/id resolution).
func newRunServiceWithAttachments(t *testing.T) (repository.Set, RunService, AttachmentService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "run.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewSet(db)
	root := filepath.Join(t.TempDir(), "attachments")
	run := NewRunService(repos, eventhub.New(), nil)
	attachments := NewAttachmentService(repos, root, WorkspaceService{repos: repos})
	run.AttachAttachmentService(&attachments)
	return repos, run, attachments
}

func createProfile(t *testing.T, repos repository.Set, supportsVision bool) model.ProviderProfile {
	t.Helper()
	profile, err := repos.Providers.Create(model.ProviderProfile{
		Name:           "test",
		Provider:       "openai_compatible",
		BaseURL:        "https://example.test",
		Model:          "vl-test",
		APIKeySecret:   "k",
		Stream:         true,
		SupportsVision: supportsVision,
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func boolPtr(b bool) *bool { return &b }

// TestProviderProfileSupportsVisionPersists guards the repository Update path,
// which must copy SupportsVision onto the loaded row (regression guard: the
// field was initially missed and silently round-tripped as false).
func TestProviderProfileSupportsVisionPersists(t *testing.T) {
	repos, _, _ := newRunServiceWithAttachments(t)
	svc := NewProviderProfileService(repos)
	created, err := svc.Create(ProviderProfileCreate{
		Name: "p", Provider: "openai_compatible", BaseURL: "https://x.test",
		Model: "m", APIKey: "k", Stream: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.SupportsVision {
		t.Fatal("new profile should default vision off")
	}
	updated, err := svc.Update(created.ID, ProviderProfileUpdate{SupportsVision: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.SupportsVision {
		t.Fatal("supports_vision not persisted through Update")
	}
	// Reload to confirm DB-level persistence.
	reloaded, err := svc.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.SupportsVision {
		t.Fatal("supports_vision not persisted at DB level")
	}
}

func TestRunStartRejectsAttachmentsWithoutVisionProfile(t *testing.T) {
	repos, run, attachments := newRunServiceWithAttachments(t)
	session, err := repos.Sessions.Ensure("sess_v", "S", "")
	if err != nil {
		t.Fatal(err)
	}
	// Upload an attachment.
	dto, err := attachments.Store(AttachmentStoreRequest{
		SessionID: session.ID,
		Reader:    bytes.NewReader(onePixelPNG(t)),
		OriginalName: "p.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := createProfile(t, repos, false) // vision OFF

	_, err = run.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input: map[string]any{
			"text": "what is this",
			"attachments": []any{
				map[string]any{"attachment_id": dto.ID},
			},
		},
		Options: map[string]any{"provider_profile_id": profile.ID},
	})
	if err == nil {
		t.Fatal("expected vision_not_supported error, got nil")
	}
}

func TestRunStartScheduleRefusesAttachments(t *testing.T) {
	repos, run, attachments := newRunServiceWithAttachments(t)
	session, err := repos.Sessions.Ensure("sess_s", "S", "")
	if err != nil {
		t.Fatal(err)
	}
	dto, err := attachments.Store(AttachmentStoreRequest{
		SessionID: session.ID, Reader: bytes.NewReader(onePixelPNG(t)), OriginalName: "p.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = run.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input: map[string]any{
			"text": "scheduled with image",
			"attachments": []any{map[string]any{"attachment_id": dto.ID}},
		},
		Options: map[string]any{"trigger_source": "schedule"},
	})
	if err == nil {
		t.Fatal("expected schedule-refuses-attachments error, got nil")
	}
}

func TestRunStartRejectsTooManyAttachments(t *testing.T) {
	repos, run, attachments := newRunServiceWithAttachments(t)
	session, err := repos.Sessions.Ensure("sess_m", "S", "")
	if err != nil {
		t.Fatal(err)
	}
	tooMany := make([]any, 0, maxAttachmentsPerRun+1)
	for i := 0; i <= maxAttachmentsPerRun; i++ {
		dto, err := attachments.Store(AttachmentStoreRequest{
			SessionID: session.ID, Reader: bytes.NewReader(onePixelPNG(t)),
			OriginalName: "p.png",
		})
		if err != nil {
			t.Fatal(err)
		}
		tooMany = append(tooMany, map[string]any{"attachment_id": dto.ID})
	}
	_, err = run.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "x", "attachments": tooMany},
	})
	if err == nil {
		t.Fatalf("expected too-many-attachments error")
	}
}

// TestPureTextRunPersistsSingleTextBlock is the zero-regression guard: a run
// with no attachments must persist the user message as a single text block,
// exactly as before (docs/51 §2.1 goal 7). Exercises prepareRun directly since
// the full Start path needs a live runtime.
func TestPureTextRunPersistsSingleTextBlock(t *testing.T) {
	repos, run, _ := newRunServiceWithAttachments(t)
	session, err := repos.Sessions.Ensure("sess_t", "S", "")
	if err != nil {
		t.Fatal(err)
	}
	admission, err := run.admitRun(protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "just text"},
	})
	if err != nil {
		t.Fatalf("admitRun: %v", err)
	}
	if _, err := run.prepareRun(admission, protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "just text"},
	}); err != nil {
		t.Fatalf("prepareRun: %v", err)
	}
	msgs, err := repos.Messages.ListLatestConversation(session.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected the user message to be persisted")
	}
	row := msgs[0]
	if row.Role != "user" {
		t.Fatalf("role = %q, want user", row.Role)
	}
	decoded := decodeContent(t, row.ContentJSON)
	if len(decoded) != 1 || decoded[0].Type != "text" {
		t.Fatalf("expected single text block, got %#v", decoded)
	}
}

// TestRunWithVisionProfilePersistsImageRefBlock verifies the happy path: with a
// vision-capable profile, a run carrying an attachment persists the user
// message as [text, image_ref] blocks.
func TestRunWithVisionProfilePersistsImageRefBlock(t *testing.T) {
	repos, run, attachments := newRunServiceWithAttachments(t)
	session, err := repos.Sessions.Ensure("sess_w", "S", "")
	if err != nil {
		t.Fatal(err)
	}
	dto, err := attachments.Store(AttachmentStoreRequest{
		SessionID: session.ID, Reader: bytes.NewReader(onePixelPNG(t)), OriginalName: "p.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := createProfile(t, repos, true) // vision ON
	payload := protows.RunStartPayload{
		SessionID: session.ID,
		Input: map[string]any{
			"text":        "describe this",
			"attachments": []any{map[string]any{"attachment_id": dto.ID}},
		},
		Options: map[string]any{"provider_profile_id": profile.ID},
	}
	admission, err := run.admitRun(payload)
	if err != nil {
		t.Fatalf("admitRun: %v", err)
	}
	params, err := run.prepareRun(admission, payload)
	if err != nil {
		t.Fatalf("prepareRun: %v", err)
	}
	// Vision flag propagated to Runtime options.
	if !params.Options.SupportsVision {
		t.Fatal("expected SupportsVision=true on params")
	}
	// ReplyInput carries the attachment ref; Slice C Strategy A inlines DataB64
	// for vision-capable profiles (ephemeral Gateway→Runtime only).
	if len(params.Input.Attachments) != 1 || params.Input.Attachments[0].AttachmentID != dto.ID {
		t.Fatalf("ReplyInput attachments = %#v", params.Input.Attachments)
	}
	if params.Input.Attachments[0].DataB64 == "" {
		t.Fatal("Slice C must inline base64 into ReplyInput for vision profiles")
	}
	if params.Input.Attachments[0].MIME == "" {
		t.Fatal("expected MIME on inlined attachment")
	}
	// Persisted user message has a text block + an image_ref block.
	msgs, err := repos.Messages.ListLatestConversation(session.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected persisted user message")
	}
	decoded := decodeContent(t, msgs[0].ContentJSON)
	if len(decoded) != 2 || decoded[0].Type != "text" || decoded[1].Type != "image_ref" {
		t.Fatalf("expected [text, image_ref], got %#v", decoded)
	}
	if decoded[1].AttachmentID != dto.ID {
		t.Fatalf("image_ref attachment_id = %q, want %q", decoded[1].AttachmentID, dto.ID)
	}
}
