package service

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreFromWorkspaceCachesAndDedupes(t *testing.T) {
	repos, svc, root := newAttachmentServiceFixture(t)
	ws := t.TempDir()
	rel := filepath.ToSlash(filepath.Join("shots", "ui.png"))
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	data := onePixelPNG(t)
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dto, err := svc.StoreFromWorkspace("sess_ws", ws, rel)
	if err != nil {
		t.Fatalf("StoreFromWorkspace: %v", err)
	}
	if dto.MIME != "image/png" || dto.ByteSize != int64(len(data)) {
		t.Fatalf("dto = %#v", dto)
	}
	// Second call with same content should return the same id (dedupe).
	dto2, err := svc.StoreFromWorkspace("sess_ws", ws, rel)
	if err != nil {
		t.Fatal(err)
	}
	if dto2.ID != dto.ID {
		t.Fatalf("dedupe failed: %s vs %s", dto.ID, dto2.ID)
	}
	row, err := repos.Attachments.Get(dto.ID, "sess_ws")
	if err != nil {
		t.Fatal(err)
	}
	if row.Kind != "workspace_cache" || row.SourcePath != rel {
		t.Fatalf("row = %#v", row)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(row.StoragePath))); err != nil {
		t.Fatalf("cached file missing: %v", err)
	}
}

func TestAttachmentCopyToSessionSharesStoragePath(t *testing.T) {
	repos, svc, root := newAttachmentServiceFixture(t)
	data := onePixelPNG(t)
	dto, err := svc.Store(AttachmentStoreRequest{
		SessionID: "sess_src", Reader: bytes.NewReader(data), OriginalName: "a.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	idMap, err := svc.CopyToSession("sess_src", "sess_dst")
	if err != nil {
		t.Fatalf("CopyToSession: %v", err)
	}
	if len(idMap) != 1 || idMap[dto.ID] == "" || idMap[dto.ID] == dto.ID {
		t.Fatalf("idMap = %#v", idMap)
	}
	src, _ := repos.Attachments.Get(dto.ID, "sess_src")
	dst, err := repos.Attachments.Get(idMap[dto.ID], "sess_dst")
	if err != nil {
		t.Fatal(err)
	}
	if src.StoragePath != dst.StoragePath {
		t.Fatalf("storage paths differ: %q vs %q", src.StoragePath, dst.StoragePath)
	}
	abs := filepath.Join(root, filepath.FromSlash(src.StoragePath))
	// Delete source session attachment; shared file must remain while dest lives.
	if err := svc.Delete(dto.ID, "sess_src"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("shared file should remain after source delete: %v", err)
	}
	if err := svc.Delete(idMap[dto.ID], "sess_dst"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("file should be removed after last ref deleted, got %v", err)
	}
}

func TestDeleteBySessionUnlinksWhenUnreferenced(t *testing.T) {
	repos, svc, root := newAttachmentServiceFixture(t)
	dto, err := svc.Store(AttachmentStoreRequest{
		SessionID: "sess_gc", Reader: bytes.NewReader(onePixelPNG(t)), OriginalName: "g.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, _ := repos.Attachments.Get(dto.ID, "sess_gc")
	abs := filepath.Join(root, filepath.FromSlash(row.StoragePath))
	if err := svc.DeleteBySession("sess_gc"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("expected unlinked file after DeleteBySession, got %v", err)
	}
	if _, err := svc.PurgeOrphans(100); err != nil {
		t.Fatal(err)
	}
}
