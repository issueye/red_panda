package service

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	protows "redpanda/protocol/ws"
)

func TestForkRewritesAttachmentIDsAndSharesBytes(t *testing.T) {
	repos, run, attachments := newRunServiceWithAttachments(t)
	session, err := repos.Sessions.Ensure("sess_fork_src", "S", "")
	if err != nil {
		t.Fatal(err)
	}
	dto, err := attachments.Store(AttachmentStoreRequest{
		SessionID: session.ID, Reader: bytes.NewReader(onePixelPNG(t)), OriginalName: "p.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := createProfile(t, repos, true)
	payload := protows.RunStartPayload{
		SessionID: session.ID,
		Input: map[string]any{
			"text":        "see image",
			"attachments": []any{map[string]any{"attachment_id": dto.ID}},
		},
		Options: map[string]any{"provider_profile_id": profile.ID},
	}
	admission, err := run.admitRun(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.prepareRun(admission, payload); err != nil {
		t.Fatal(err)
	}

	svc := NewSessionService(repos, nil, nil, t.TempDir())
	svc.AttachAttachmentService(&attachments)
	result, err := svc.Fork(session.ID, ForkSessionRequest{Name: "forked"})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	forkMsgs, err := repos.Messages.ListLatestConversation(result.Session.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(forkMsgs) == 0 {
		t.Fatal("no forked messages")
	}
	var foundNewID string
	for _, msg := range forkMsgs {
		decoded := decodeContent(t, msg.ContentJSON)
		for _, block := range decoded {
			if block.Type != "image_ref" {
				continue
			}
			if block.AttachmentID == dto.ID {
				t.Fatalf("forked message still points at source attachment id %s", dto.ID)
			}
			foundNewID = block.AttachmentID
		}
	}
	if foundNewID == "" {
		t.Fatal("expected rewritten image_ref in forked messages")
	}
	if _, data, _, err := attachments.Bytes(dto.ID, session.ID); err != nil || len(data) == 0 {
		t.Fatalf("source bytes: %v", err)
	}
	if _, data, _, err := attachments.Bytes(foundNewID, result.Session.ID); err != nil || len(data) == 0 {
		t.Fatalf("fork bytes: %v", err)
	}
}

func TestRunStartAcceptsWorkspacePathAttachment(t *testing.T) {
	repos, run, _ := newRunServiceWithAttachments(t)
	ws := t.TempDir()
	rel := "diagram.png"
	if err := os.WriteFile(filepath.Join(ws, rel), onePixelPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := repos.Sessions.Ensure("sess_path", "S", ws)
	if err != nil {
		t.Fatal(err)
	}
	profile := createProfile(t, repos, true)
	payload := protows.RunStartPayload{
		SessionID: session.ID,
		Input: map[string]any{
			"text":        "explain diagram",
			"attachments": []any{map[string]any{"path": rel}},
		},
		Options: map[string]any{"provider_profile_id": profile.ID, "working_dir": ws},
	}
	admission, err := run.admitRun(payload)
	if err != nil {
		t.Fatalf("admitRun path ref: %v", err)
	}
	params, err := run.prepareRun(admission, payload)
	if err != nil {
		t.Fatalf("prepareRun path ref: %v", err)
	}
	if len(params.Input.Attachments) != 1 || params.Input.Attachments[0].Path != rel {
		t.Fatalf("attachments = %#v", params.Input.Attachments)
	}
	if params.Input.Attachments[0].DataB64 == "" {
		t.Fatal("expected Strategy A inline for path ref under vision profile")
	}
}
