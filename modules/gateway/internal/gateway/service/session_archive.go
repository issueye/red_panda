package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

const sessionArchiveFormatVersion = 1

// DefaultSessionArchiveDir returns {dir(databaseDSN)}/session_archives.
func DefaultSessionArchiveDir(databaseDSN string) string {
	dsn := strings.TrimSpace(databaseDSN)
	if dsn == "" {
		return filepath.Join(".", "session_archives")
	}
	if i := strings.Index(dsn, "?"); i >= 0 {
		dsn = dsn[:i]
	}
	dir := filepath.Dir(dsn)
	if dir == "" || dir == "." {
		return filepath.Join(".", "session_archives")
	}
	return filepath.Join(dir, "session_archives")
}

type archiveLine struct {
	V    int    `json:"v"`
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// archiveSessionJSONL writes a full session snapshot as JSONL under archiveDir.
// Returns the written file path. Does not modify the database.
func archiveSessionJSONL(repos repository.Set, archiveDir, sessionID, reason string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(archiveDir) == "" {
		return "", fmt.Errorf("session archive directory is not configured")
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}

	session, err := repos.Sessions.GetAny(sessionID)
	if err != nil {
		return "", fmt.Errorf("load session: %w", err)
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")
	safeID := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, sessionID)
	path := filepath.Join(archiveDir, fmt.Sprintf("%s_%s.jsonl", safeID, stamp))

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("create archive file: %w", err)
	}
	defer f.Close()

	write := func(typ string, data any) error {
		line, err := json.Marshal(archiveLine{V: sessionArchiveFormatVersion, Type: typ, Data: data})
		if err != nil {
			return err
		}
		_, err = f.Write(append(line, '\n'))
		return err
	}

	if err := write("archive_header", map[string]any{
		"archived_at": time.Now().UTC().Format(time.RFC3339Nano),
		"reason":      reason,
		"session_id":  sessionID,
	}); err != nil {
		return "", err
	}
	if err := write("session", session); err != nil {
		return "", err
	}

	messages, err := repos.Messages.ListAll(sessionID)
	if err != nil {
		return "", err
	}
	for i := range messages {
		if err := write("message", messages[i]); err != nil {
			return "", err
		}
	}

	var runs []model.RunRecord
	if err := repos.DB.Where("session_id = ?", sessionID).Order("started_at asc").Find(&runs).Error; err != nil {
		return "", err
	}
	runIDs := make([]string, 0, len(runs))
	for i := range runs {
		runIDs = append(runIDs, runs[i].ID)
		if err := write("run", runs[i]); err != nil {
			return "", err
		}
	}
	events, err := repos.RunEvents.ListByRunIDs(runIDs)
	if err != nil {
		return "", err
	}
	for i := range events {
		if err := write("run_event", events[i]); err != nil {
			return "", err
		}
	}

	var tools []model.ToolCall
	if err := repos.DB.Where("session_id = ?", sessionID).Order("started_at asc").Find(&tools).Error; err != nil {
		return "", err
	}
	for i := range tools {
		if err := write("tool_call", tools[i]); err != nil {
			return "", err
		}
	}

	var perms []model.PermissionRequest
	if err := repos.DB.Where("session_id = ?", sessionID).Order("created_at asc").Find(&perms).Error; err != nil {
		return "", err
	}
	for i := range perms {
		if err := write("permission", perms[i]); err != nil {
			return "", err
		}
	}

	todos, err := repos.Todos.ListBySession(sessionID)
	if err != nil {
		return "", err
	}
	for i := range todos {
		if err := write("todo", todos[i]); err != nil {
			return "", err
		}
	}

	var goals []model.Goal
	if err := repos.DB.Where("session_id = ?", sessionID).Order("created_at asc").Find(&goals).Error; err != nil {
		return "", err
	}
	goalIDs := make([]string, 0, len(goals))
	for i := range goals {
		goalIDs = append(goalIDs, goals[i].ID)
		if err := write("goal", goals[i]); err != nil {
			return "", err
		}
	}
	if len(goalIDs) > 0 {
		var notes []model.GoalNote
		if err := repos.DB.Where("goal_id IN ?", goalIDs).Order("seq asc").Find(&notes).Error; err != nil {
			return "", err
		}
		for i := range notes {
			if err := write("goal_note", notes[i]); err != nil {
				return "", err
			}
		}
		var actions []model.GoalAction
		if err := repos.DB.Where("goal_id IN ?", goalIDs).Order("sort_order asc").Find(&actions).Error; err != nil {
			return "", err
		}
		for i := range actions {
			if err := write("goal_action", actions[i]); err != nil {
				return "", err
			}
		}
		var gevents []model.GoalEvent
		if err := repos.DB.Where("goal_id IN ?", goalIDs).Order("seq asc").Find(&gevents).Error; err != nil {
			return "", err
		}
		for i := range gevents {
			if err := write("goal_event", gevents[i]); err != nil {
				return "", err
			}
		}
		var segs []model.GoalSegment
		if err := repos.DB.Where("goal_id IN ?", goalIDs).Order("created_at asc").Find(&segs).Error; err != nil {
			return "", err
		}
		for i := range segs {
			if err := write("goal_segment", segs[i]); err != nil {
				return "", err
			}
		}
	}

	compactions, err := repos.Compactions.ListBySessions([]string{sessionID})
	if err != nil {
		return "", err
	}
	for i := range compactions {
		if err := write("compaction", compactions[i]); err != nil {
			return "", err
		}
	}

	lineage, err := repos.Lineage.ListBySessions([]string{sessionID})
	if err != nil {
		return "", err
	}
	for i := range lineage {
		if err := write("lineage", lineage[i]); err != nil {
			return "", err
		}
	}

	schedRuns, err := repos.Schedules.ListScheduledRunsBySessions([]string{sessionID})
	if err != nil {
		return "", err
	}
	for i := range schedRuns {
		if err := write("schedule_run", schedRuns[i]); err != nil {
			return "", err
		}
	}

	// Memory: snapshot only — durable records stay in SQLite.
	memories, err := repos.Memory.ListBySessionIDs([]string{sessionID})
	if err != nil {
		return "", err
	}
	for i := range memories {
		if err := write("memory_ref", memories[i]); err != nil {
			return "", err
		}
	}

	if err := f.Sync(); err != nil {
		return "", err
	}
	return path, nil
}
