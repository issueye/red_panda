// AttachmentService owns the Gateway-authoritative image asset lifecycle
// (docs/51 §4, §5, §9). It stores binary bytes on disk under AttachmentsDir
// and keeps only metadata + references in SQLite so messages hold refs, never
// base64 blobs. Multimodal *delivery* to the model is a Runtime concern; this
// service is storage + resolution only.

package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "image/gif"  // register decoders so DecodeConfig sniffs these types
	_ "image/jpeg"
	_ "image/png"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

const (
	// AttachmentMaxBytes is the per-file hard cap (docs/51 §9: 8 MiB).
	AttachmentMaxBytes = 8 * 1024 * 1024
	// AttachmentSessionQuotaBytes is the per-session soft cap (docs/51 §9: 512 MiB).
	AttachmentSessionQuotaBytes = 512 * 1024 * 1024
)

// allowedAttachmentMIME maps a sniffed/detected MIME to its on-disk extension.
// SVG is intentionally absent (script/parse risk, docs/51 §9).
var allowedAttachmentMIME = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

type AttachmentService struct {
	repos        repository.Set
	storageRoot  string // absolute; from DefaultAttachmentsDir(dsn) or env
	workspaceSrv WorkspaceService
}

func NewAttachmentService(repos repository.Set, storageRoot string, workspaceSrv WorkspaceService) AttachmentService {
	return AttachmentService{repos: repos, storageRoot: storageRoot, workspaceSrv: workspaceSrv}
}

// AttachmentRef is the parallel-render reference surfaced on message DTOs so
// the Desktop renders thumbnails without touching the text pipeline.
type AttachmentRef struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	MIME       string `json:"mime"`
	ByteSize   int64  `json:"byte_size"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Alt        string `json:"alt,omitempty"`
	Path       string `json:"path,omitempty"` // workspace-relative path (kind=path-ref only)
	OriginalName string `json:"original_name,omitempty"`
	URL        string `json:"url"` // GET /api/v1/attachments/:id (auth required)
}

// AttachmentDTO is the full metadata response (no base64).
type AttachmentDTO struct {
	AttachmentRef
	SessionID string    `json:"session_id"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

type AttachmentStoreRequest struct {
	SessionID     string
	WorkspaceRoot string
	Reader        io.Reader
	OriginalName  string
	Alt           string
	CreatedBy     string
}

// Store validates, sniffs, hashes, persists bytes to disk, and inserts the row.
func (s AttachmentService) Store(input AttachmentStoreRequest) (AttachmentDTO, error) {
	if strings.TrimSpace(input.SessionID) == "" {
		return AttachmentDTO{}, fmt.Errorf("session id is required")
	}
	createdBy := strings.TrimSpace(input.CreatedBy)
	if createdBy == "" {
		createdBy = "user"
	}

	// Read fully (cap enforced). Held in memory; max is 8 MiB.
	buf := &bytes.Buffer{}
	limited := &io.LimitedReader{R: input.Reader, N: AttachmentMaxBytes + 1}
	if _, err := io.Copy(buf, limited); err != nil {
		return AttachmentDTO{}, fmt.Errorf("read attachment: %w", err)
	}
	if int64(buf.Len()) > AttachmentMaxBytes {
		return AttachmentDTO{}, fmt.Errorf("attachment exceeds %d bytes", AttachmentMaxBytes)
	}
	data := buf.Bytes()

	mimeStr, ext, width, height, err := sniffImage(data, input.OriginalName)
	if err != nil {
		return AttachmentDTO{}, err
	}

	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	// Per-session quota.
	used, err := s.repos.Attachments.SumBytesBySession(input.SessionID)
	if err != nil {
		return AttachmentDTO{}, err
	}
	if used+int64(len(data)) > AttachmentSessionQuotaBytes {
		return AttachmentDTO{}, fmt.Errorf("session attachment quota exceeded (%d bytes)", AttachmentSessionQuotaBytes)
	}

	storagePath, err := s.persist(input.SessionID, sha, ext, data)
	if err != nil {
		return AttachmentDTO{}, err
	}

	row, err := s.repos.Attachments.Create(model.Attachment{
		ID:            newAttachmentID(),
		SessionID:     input.SessionID,
		WorkspaceRoot: input.WorkspaceRoot,
		Kind:          "upload",
		StoragePath:   storagePath,
		MIME:          mimeStr,
		ByteSize:      int64(len(data)),
		SHA256:        sha,
		Width:         width,
		Height:        height,
		OriginalName:  input.OriginalName,
		CreatedBy:     createdBy,
	})
	if err != nil {
		// Best-effort cleanup of the orphaned file.
		_ = os.Remove(filepath.Join(s.storageRoot, storagePath))
		return AttachmentDTO{}, err
	}
	return attachmentDTO(row), nil
}

// ListBySession returns metadata for a session's live attachments (no bytes).
func (s AttachmentService) ListBySession(sessionID string) ([]AttachmentDTO, error) {
	rows, err := s.repos.Attachments.ListBySession(sessionID)
	if err != nil {
		return nil, err
	}
	items := make([]AttachmentDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, attachmentDTO(row))
	}
	return items, nil
}

// persist writes bytes to <root>/<safeSession>/<sha-prefix>_<sha>.<ext> and
// returns the storage-root-relative path.
func (s AttachmentService) persist(sessionID, sha, ext string, data []byte) (string, error) {
	relDir := filepath.Join(safeSegment(sessionID), sha[:2])
	absDir := filepath.Join(s.storageRoot, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("create attachment dir: %w", err)
	}
	name := fmt.Sprintf("%s%s", sha, ext)
	relPath := filepath.Join(relDir, name)
	absPath := filepath.Join(s.storageRoot, relPath)

	// Exclusive create: dedupe by sha within the same session dir.
	f, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("create attachment file: %w", err)
	}
	if err == nil {
		if _, err := f.Write(data); err != nil {
			f.Close()
			_ = os.Remove(absPath)
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
	}
	return filepath.ToSlash(relPath), nil
}

func (s AttachmentService) Meta(id string, sessionID string) (AttachmentDTO, error) {
	row, err := s.repos.Attachments.Get(id, sessionID)
	if err != nil {
		return AttachmentDTO{}, err
	}
	return attachmentDTO(row), nil
}

// Bytes streams the stored file content.
func (s AttachmentService) Bytes(id string, sessionID string) (mimeStr string, data []byte, sha string, err error) {
	row, err := s.repos.Attachments.Get(id, sessionID)
	if err != nil {
		return "", nil, "", err
	}
	abs := filepath.Join(s.storageRoot, filepath.FromSlash(row.StoragePath))
	data, err = os.ReadFile(abs)
	if err != nil {
		return "", nil, "", err
	}
	return row.MIME, data, row.SHA256, nil
}

// Delete soft-deletes the row and unlinks the physical file if no other row
// points at the same storage path (deduped uploads share files).
func (s AttachmentService) Delete(id string, sessionID string) error {
	row, err := s.repos.Attachments.Get(id, sessionID)
	if err != nil {
		return err
	}
	if err := s.repos.Attachments.SoftDelete(id); err != nil {
		return err
	}
	// Count remaining live rows sharing this file before unlinking.
	var remaining int64
	if err := s.repos.DB.Model(&model.Attachment{}).
		Where("storage_path = ? AND deleted_at IS NULL", row.StoragePath).
		Count(&remaining).Error; err != nil {
		return err
	}
	if remaining == 0 {
		_ = os.Remove(filepath.Join(s.storageRoot, filepath.FromSlash(row.StoragePath)))
	}
	return nil
}

// DeleteBySession cascades on session hard-delete (docs/51 §10). Best-effort
// file unlink per shared storage path.
func (s AttachmentService) DeleteBySession(sessionID string) error {
	rows, err := s.repos.Attachments.ListBySession(sessionID)
	if err != nil {
		return err
	}
	if _, err := s.repos.Attachments.SoftDeleteBySession(sessionID); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		if _, ok := seen[row.StoragePath]; ok {
			continue
		}
		seen[row.StoragePath] = struct{}{}
		var remaining int64
		if err := s.repos.DB.Model(&model.Attachment{}).
			Where("storage_path = ? AND deleted_at IS NULL", row.StoragePath).
			Count(&remaining).Error; err != nil {
			continue
		}
		if remaining == 0 {
			_ = os.Remove(filepath.Join(s.storageRoot, filepath.FromSlash(row.StoragePath)))
		}
	}
	return nil
}

// PurgeOrphans removes storage files whose rows are soft-deleted (startup GC).
func (s AttachmentService) PurgeOrphans(limit int) (int, error) {
	rows, err := s.repos.Attachments.ListOrphans(limit)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, row := range rows {
		abs := filepath.Join(s.storageRoot, filepath.FromSlash(row.StoragePath))
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			continue
		}
		if err := s.repos.Attachments.DeletePermanently(row.ID); err == nil {
			removed++
		}
	}
	return removed, nil
}

// CopyToSession forks attachment metadata into the target session while
// sharing storage_path (docs/51 §10 / docs/52 Slice D). Returns source→target
// attachment id map so callers can rewrite image_ref blocks in copied messages.
func (s AttachmentService) CopyToSession(sourceSessionID, targetSessionID string) (map[string]string, error) {
	if strings.TrimSpace(sourceSessionID) == "" || strings.TrimSpace(targetSessionID) == "" {
		return nil, fmt.Errorf("source and target session ids are required")
	}
	if sourceSessionID == targetSessionID {
		return map[string]string{}, nil
	}
	return s.repos.Attachments.CopyToSession(sourceSessionID, targetSessionID)
}

// DeleteBySessions soft-deletes attachments for many sessions then unlinks
// files that no longer have any live row pointing at the same storage_path.
func (s AttachmentService) DeleteBySessions(sessionIDs []string) error {
	for _, sessionID := range sessionIDs {
		if err := s.DeleteBySession(sessionID); err != nil {
			return err
		}
	}
	return nil
}

// StoreFromWorkspace optionally caches a workspace image as an attachment row
// (docs/52 Slice D from-workspace API). The file is copied into attachment
// storage under kind=workspace_cache; sha256 dedupe reuses an existing row in
// the same session when content matches.
func (s AttachmentService) StoreFromWorkspace(sessionID, workspaceRoot, relPath string) (AttachmentDTO, error) {
	if strings.TrimSpace(sessionID) == "" {
		return AttachmentDTO{}, fmt.Errorf("session id is required")
	}
	abs, err := s.ResolveWorkspacePath(workspaceRoot, relPath)
	if err != nil {
		return AttachmentDTO{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return AttachmentDTO{}, fmt.Errorf("workspace image %s: %w", relPath, err)
	}
	if info.IsDir() {
		return AttachmentDTO{}, fmt.Errorf("workspace path %s is a directory", relPath)
	}
	if info.Size() > AttachmentMaxBytes {
		return AttachmentDTO{}, fmt.Errorf("attachment exceeds %d bytes", AttachmentMaxBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return AttachmentDTO{}, err
	}
	mimeStr, ext, width, height, err := sniffImage(data, relPath)
	if err != nil {
		return AttachmentDTO{}, err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	// Prefer an existing live row in this session with the same content hash.
	if existing, err := s.repos.Attachments.FindBySHA256(sha, sessionID); err == nil && existing.SessionID == sessionID {
		_ = s.repos.Attachments.TouchLastRef(existing.ID, time.Now().UTC())
		return attachmentDTO(existing), nil
	}

	used, err := s.repos.Attachments.SumBytesBySession(sessionID)
	if err != nil {
		return AttachmentDTO{}, err
	}
	if used+int64(len(data)) > AttachmentSessionQuotaBytes {
		return AttachmentDTO{}, fmt.Errorf("session attachment quota exceeded (%d bytes)", AttachmentSessionQuotaBytes)
	}

	storagePath, err := s.persist(sessionID, sha, ext, data)
	if err != nil {
		return AttachmentDTO{}, err
	}
	row, err := s.repos.Attachments.Create(model.Attachment{
		ID:            newAttachmentID(),
		SessionID:     sessionID,
		WorkspaceRoot: workspaceRoot,
		Kind:          "workspace_cache",
		SourcePath:    relPath,
		StoragePath:   storagePath,
		MIME:          mimeStr,
		ByteSize:      int64(len(data)),
		SHA256:        sha,
		Width:         width,
		Height:        height,
		OriginalName:  filepath.Base(relPath),
		CreatedBy:     "user",
	})
	if err != nil {
		_ = os.Remove(filepath.Join(s.storageRoot, storagePath))
		return AttachmentDTO{}, err
	}
	return attachmentDTO(row), nil
}

// ResolveWorkspacePath validates a workspace-relative path reference for an
// image_ref block. It mirrors WorkspaceService.resolvePath but additionally
// evaluates symlinks so a link that escapes the workspace root is rejected
// (docs/51 §4 — gap noted against the lexical-only workspace helper).
func (s AttachmentService) ResolveWorkspacePath(workspaceRoot, relPath string) (absPath string, err error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", errors.New("path is required")
	}
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		current, err := s.workspaceSrv.Current()
		if err != nil {
			return "", err
		}
		if current == nil {
			return "", errors.New("workspace is not open")
		}
		root = current.RootPath
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanRel := filepath.Clean(relPath)
	if filepath.IsAbs(cleanRel) {
		return "", errors.New("path must be relative")
	}
	joined := filepath.Join(absRoot, cleanRel)
	absTarget, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	// Lexical traversal check.
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes workspace root")
	}
	// Symlink resolution: the resolved real path must remain under the real root.
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		// Non-existent root: fall back to lexical result (caller's workspace is broken).
		return absTarget, nil
	}
	realTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		// Target doesn't exist yet — caller will surface a clear error on read.
		return absTarget, nil
	}
	relReal, err := filepath.Rel(realRoot, realTarget)
	if err != nil {
		return "", err
	}
	if relReal == ".." || strings.HasPrefix(relReal, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes workspace root (symlink)")
	}
	return absTarget, nil
}

// TouchLastRef records that an attachment was referenced (GC recency helper).
func (s AttachmentService) TouchLastRef(id string) error {
	return s.repos.Attachments.TouchLastRef(id, time.Now().UTC())
}

// ResolveWorkspacePathAbs is the WorkspaceService-backed path resolver exposed
// for run.start admission (lexical + symlink evaluation, docs/51 §4).
func (s AttachmentService) ResolveWorkspacePathAbs(workspaceRoot, relPath string) (string, error) {
	return s.ResolveWorkspacePath(workspaceRoot, relPath)
}

// sniffImageFile reads a file's header and returns its detected mime, extension,
// and dimensions. Used for workspace path references (not persisted bytes).
func sniffImageFile(absPath string) (mimeStr, ext string, width, height int, err error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", "", 0, 0, err
	}
	defer f.Close()
	// Read enough for image.DecodeConfig on all supported formats (4KiB is plenty
	// for headers; PNG/JPEG/GIF headers fit in a few hundred bytes).
	header := make([]byte, 8192)
	n, _ := f.Read(header)
	data := header[:n]
	mimeStr, ext, width, height, err = sniffImage(data, absPath)
	return mimeStr, ext, width, height, err
}

func (s AttachmentService) Root() string { return s.storageRoot }

// AbsoluteStoragePath joins a stored relative path with the attachments root.
// Used by run.start Strategy A inlining (docs/52 Slice C).
func (s AttachmentService) AbsoluteStoragePath(storagePath string) string {
	if strings.TrimSpace(storagePath) == "" {
		return ""
	}
	return filepath.Join(s.storageRoot, filepath.FromSlash(storagePath))
}

func newAttachmentID() string {
	return fmt.Sprintf("att_%d", time.Now().UnixNano())
}

// sniffImage verifies the bytes are a real allowed image (magic bytes, not just
// extension) and returns mime, extension, dimensions. Uses image.DecodeConfig
// which reads headers only.
func sniffImage(data []byte, originalName string) (mimeStr, ext string, width, height int, err error) {
	cfg, format, derr := image.DecodeConfig(bytes.NewReader(data))
	if derr != nil {
		return "", "", 0, 0, fmt.Errorf("file is not a supported image (png/jpeg/webp/gif)")
	}
	mimeStr = "image/" + format // DecodeConfig returns "png"|"jpeg"|"gif"; webp unsupported by stdlib
	if _, ok := allowedAttachmentMIME[mimeStr]; !ok {
		// webp is not decoded by stdlib; fall back to content-type sniffing.
		mimeStr = http.DetectContentType(data)
		if _, ok2 := allowedAttachmentMIME[mimeStr]; !ok2 {
			return "", "", 0, 0, fmt.Errorf("unsupported image type %q (allowed: png/jpeg/webp/gif)", mimeStr)
		}
	}
	ext, ok := allowedAttachmentMIME[mimeStr]
	if !ok {
		ext = imageExtFromName(originalName, mimeStr)
	}
	if cfg.Width > 0 && cfg.Height > 0 {
		width, height = cfg.Width, cfg.Height
	}
	return mimeStr, ext, width, height, nil
}

func imageExtFromName(name, mimeStr string) string {
	switch mimeStr {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			return name[dot:]
		}
		return ".bin"
	}
}

// attachmentDTO builds the response DTO (no base64).
func attachmentDTO(row model.Attachment) AttachmentDTO {
	ref := AttachmentRef{
		ID:           row.ID,
		Kind:         row.Kind,
		MIME:         row.MIME,
		ByteSize:     row.ByteSize,
		Width:        row.Width,
		Height:       row.Height,
		Alt:          row.OriginalName,
		OriginalName: row.OriginalName,
		Path:         row.SourcePath,
		URL:          fmt.Sprintf("/api/v1/attachments/%s", row.ID),
	}
	if ref.Kind == "" {
		ref.Kind = "upload"
	}
	return AttachmentDTO{
		AttachmentRef: ref,
		SessionID:     row.SessionID,
		SHA256:        row.SHA256,
		CreatedAt:     row.CreatedAt,
	}
}

// AttachmentRefFromBlock maps a protocol image_ref ContentBlock to the parallel
// DTO field the Desktop renders. width/height/mime may be 0/"" if the stored
// block lacks them; the URL is rebuilt from the id.
func AttachmentRefFromBlock(block blockLike) AttachmentRef {
	ref := AttachmentRef{
		ID:           block.AttachmentID,
		Kind:         "upload",
		MIME:         block.MIME,
		ByteSize:     block.ByteSize,
		Width:        block.Width,
		Height:       block.Height,
		Alt:          block.Alt,
		Path:         block.Path,
		OriginalName: block.Alt,
	}
	if block.Path != "" && block.AttachmentID == "" {
		ref.Kind = "workspace_path"
	}
	if ref.ID != "" {
		ref.URL = fmt.Sprintf("/api/v1/attachments/%s", ref.ID)
	}
	return ref
}

// blockLike decouples AttachmentRefFromBlock from the protocol package import
// cycle concerns; the caller passes a methods.ContentBlock adapter.
type blockLike struct {
	AttachmentID string
	Path         string
	MIME         string
	Alt          string
	Width        int
	Height       int
	ByteSize     int64
}
