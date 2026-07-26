package service

import (
	"path/filepath"
	"strings"
)

// DefaultAttachmentsDir returns {dir(databaseDSN)}/attachments, mirroring
// DefaultSessionArchiveDir (docs/49). The attachment storage root is the
// Gateway data dir; per-session files live under <root>/<session_id>/.
func DefaultAttachmentsDir(databaseDSN string) string {
	dsn := strings.TrimSpace(databaseDSN)
	if dsn == "" {
		return filepath.Join(".", "attachments")
	}
	if i := strings.Index(dsn, "?"); i >= 0 {
		dsn = dsn[:i]
	}
	dir := filepath.Dir(dsn)
	if dir == "" || dir == "." {
		return filepath.Join(".", "attachments")
	}
	return filepath.Join(dir, "attachments")
}

// safeSegment maps an arbitrary session id to a filesystem-safe single path
// component so <session_id>/<id>.<ext> can never escape the storage root.
func safeSegment(id string) string {
	if id == "" {
		return "_"
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, id)
}
