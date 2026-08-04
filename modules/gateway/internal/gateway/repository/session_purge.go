package repository

import (
	"redpanda/gateway/internal/gateway/model"
)

// HardDeleteMany permanently removes session rows (no soft-delete).
func (r SessionRepository) HardDeleteMany(sessionIDs []string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}
	res := r.db.Where("id IN ?", sessionIDs).Delete(&model.Session{})
	return res.RowsAffected, res.Error
}

// DeleteBySessions removes all messages for the given sessions.
func (r MessageRepository) DeleteBySessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	return r.db.Where("session_id IN ?", sessionIDs).Delete(&model.Message{}).Error
}

// DeleteBySessions removes tool calls for the sessions.
func (r ToolCallRepository) DeleteBySessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	return r.db.Where("session_id IN ?", sessionIDs).Delete(&model.ToolCall{}).Error
}

// DeleteByRunIDs removes tool calls belonging to the given runs.
func (r ToolCallRepository) DeleteByRunIDs(runIDs []string) error {
	if len(runIDs) == 0 {
		return nil
	}
	return r.db.Where("run_id IN ?", runIDs).Delete(&model.ToolCall{}).Error
}

// DeleteBySessions removes permission rows for the sessions.
func (r PermissionRequestRepository) DeleteBySessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	return r.db.Where("session_id IN ?", sessionIDs).Delete(&model.PermissionRequest{}).Error
}

// DeleteByRunIDs removes permission rows belonging to the given runs.
func (r PermissionRequestRepository) DeleteByRunIDs(runIDs []string) error {
	if len(runIDs) == 0 {
		return nil
	}
	return r.db.Where("run_id IN ?", runIDs).Delete(&model.PermissionRequest{}).Error
}

// DeleteBySessions removes run records for the sessions.
func (r RunRecordRepository) DeleteBySessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	return r.db.Where("session_id IN ?", sessionIDs).Delete(&model.RunRecord{}).Error
}

// DeleteByRunIDs removes run records with the given ids.
func (r RunRecordRepository) DeleteByRunIDs(runIDs []string) error {
	if len(runIDs) == 0 {
		return nil
	}
	return r.db.Where("id IN ?", runIDs).Delete(&model.RunRecord{}).Error
}

// ListIDsBySessions returns run ids owned by the sessions.
func (r RunRecordRepository) ListIDsBySessions(sessionIDs []string) ([]string, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var ids []string
	err := r.db.Model(&model.RunRecord{}).Where("session_id IN ?", sessionIDs).Pluck("id", &ids).Error
	return ids, err
}

// DeleteByRunIDs removes run events for the given runs.
func (r RunEventRepository) DeleteByRunIDs(runIDs []string) error {
	if len(runIDs) == 0 {
		return nil
	}
	return r.db.Where("run_id IN ?", runIDs).Delete(&model.RunEvent{}).Error
}

// ListByRunIDs returns run events for archival (ordered by run/seq).
func (r RunEventRepository) ListByRunIDs(runIDs []string) ([]model.RunEvent, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}
	var rows []model.RunEvent
	err := r.db.Where("run_id IN ?", runIDs).Order("run_id asc, run_seq asc").Find(&rows).Error
	return rows, err
}

// DeleteBySessions removes compactions referencing the sessions as source or target.
func (r SessionCompactionRepository) DeleteBySessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	return r.db.Where("source_session_id IN ? OR target_session_id IN ?", sessionIDs, sessionIDs).
		Delete(&model.SessionCompaction{}).Error
}

// ListBySessions lists compactions for archive.
func (r SessionCompactionRepository) ListBySessions(sessionIDs []string) ([]model.SessionCompaction, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var rows []model.SessionCompaction
	err := r.db.Where("source_session_id IN ? OR target_session_id IN ?", sessionIDs, sessionIDs).
		Order("created_at asc").Find(&rows).Error
	return rows, err
}

// DeleteBySessions removes lineage rows referencing the sessions.
func (r SessionLineageRepository) DeleteBySessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	return r.db.Where("source_session_id IN ? OR target_session_id IN ?", sessionIDs, sessionIDs).
		Delete(&model.SessionLineage{}).Error
}

// ListBySessions lists lineage for archive.
func (r SessionLineageRepository) ListBySessions(sessionIDs []string) ([]model.SessionLineage, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var rows []model.SessionLineage
	err := r.db.Where("source_session_id IN ? OR target_session_id IN ?", sessionIDs, sessionIDs).
		Order("created_at asc").Find(&rows).Error
	return rows, err
}

// DeleteScheduledRunsBySessions removes schedule fire audit rows for sessions.
func (r ScheduleRepository) DeleteScheduledRunsBySessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	return r.db.Where("session_id IN ?", sessionIDs).Delete(&model.ScheduledTaskRun{}).Error
}

// ListScheduledRunsBySessions for archive.
func (r ScheduleRepository) ListScheduledRunsBySessions(sessionIDs []string) ([]model.ScheduledTaskRun, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var rows []model.ScheduledTaskRun
	err := r.db.Where("session_id IN ?", sessionIDs).Order("created_at asc").Find(&rows).Error
	return rows, err
}

// ListBySessionIDs lists memory rows tagged with the session (for archive only).
func (r MemoryRepository) ListBySessionIDs(sessionIDs []string) ([]model.MemoryRecord, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var rows []model.MemoryRecord
	err := r.db.Where("session_id IN ? AND deleted_at IS NULL", sessionIDs).
		Order("created_at asc").Find(&rows).Error
	return rows, err
}

// ListIncludingDeleted gets a session even if soft-deleted (for archive of legacy rows).
func (r SessionRepository) GetAny(id string) (model.Session, error) {
	var session model.Session
	err := r.db.Where("id = ?", id).First(&session).Error
	return session, err
}

// HardDeleteCascade permanently removes session-scoped rows for the given ids.
// Caller must archive first. Order avoids leaving orphans in common SQLite setups.
func (s Set) HardDeleteCascade(sessionIDs []string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}
	runIDs, err := s.Runs.ListIDsBySessions(sessionIDs)
	if err != nil {
		return 0, err
	}
	if err := s.RunEvents.DeleteByRunIDs(runIDs); err != nil {
		return 0, err
	}
	if err := s.ToolCalls.DeleteBySessions(sessionIDs); err != nil {
		return 0, err
	}
	if err := s.Permissions.DeleteBySessions(sessionIDs); err != nil {
		return 0, err
	}
	if err := s.Messages.DeleteBySessions(sessionIDs); err != nil {
		return 0, err
	}
	if err := s.Todos.DeleteBySessions(sessionIDs); err != nil {
		return 0, err
	}
	if err := s.Compactions.DeleteBySessions(sessionIDs); err != nil {
		return 0, err
	}
	if err := s.Lineage.DeleteBySessions(sessionIDs); err != nil {
		return 0, err
	}
	if err := s.Schedules.DeleteScheduledRunsBySessions(sessionIDs); err != nil {
		return 0, err
	}
	if err := s.Runs.DeleteBySessions(sessionIDs); err != nil {
		return 0, err
	}
	if _, err := s.Schedules.DisableFixedForSessions(sessionIDs); err != nil {
		return 0, err
	}
	return s.Sessions.HardDeleteMany(sessionIDs)
}
