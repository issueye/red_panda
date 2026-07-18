package repository

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

type GoalRepository struct {
	db *gorm.DB
}

func NewGoalRepository(db *gorm.DB) GoalRepository {
	return GoalRepository{db: db}
}

func (r GoalRepository) Create(row model.Goal) (model.Goal, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(row.ID) == "" {
		row.ID = fmt.Sprintf("goal_%d", now.UnixNano())
	}
	if row.Status == "" {
		row.Status = "pending"
	}
	applyGoalBudgetDefaults(&row)
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := r.db.Create(&row).Error; err != nil {
		return model.Goal{}, err
	}
	return row, nil
}

func (r GoalRepository) Update(row model.Goal) (model.Goal, error) {
	row.UpdatedAt = time.Now().UTC()
	if err := r.db.Save(&row).Error; err != nil {
		return model.Goal{}, err
	}
	return row, nil
}

func (r GoalRepository) Get(id string) (model.Goal, error) {
	var row model.Goal
	err := r.db.First(&row, "id = ?", id).Error
	return row, err
}

func (r GoalRepository) ListBySession(sessionID string, limit int) ([]model.Goal, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.Goal
	err := r.db.Where("session_id = ?", sessionID).
		Order("updated_at desc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r GoalRepository) CountBySession(sessionID string) (int64, error) {
	var n int64
	err := r.db.Model(&model.Goal{}).Where("session_id = ?", sessionID).Count(&n).Error
	return n, err
}

func (r GoalRepository) GetActiveBySession(sessionID string) (model.Goal, error) {
	var row model.Goal
	err := r.db.Where("session_id = ? AND status = ?", sessionID, "active").
		Order("updated_at desc").
		First(&row).Error
	return row, err
}

func (r GoalRepository) GetLatestPausedBySession(sessionID string) (model.Goal, error) {
	var row model.Goal
	err := r.db.Where("session_id = ? AND status = ?", sessionID, "paused").
		Order("updated_at desc").
		First(&row).Error
	return row, err
}

func (r GoalRepository) GetLatestPendingBySession(sessionID string) (model.Goal, error) {
	var row model.Goal
	err := r.db.Where("session_id = ? AND status = ?", sessionID, "pending").
		Order("updated_at desc").
		First(&row).Error
	return row, err
}

func (r GoalRepository) FindByActiveRunID(runID string) (model.Goal, error) {
	var row model.Goal
	err := r.db.Where("active_run_id = ?", runID).First(&row).Error
	return row, err
}

func (r GoalRepository) FindByLastRunID(runID string) (model.Goal, error) {
	var row model.Goal
	err := r.db.Where("last_run_id = ?", runID).Order("updated_at desc").First(&row).Error
	return row, err
}

// RecordSegment inserts one accounting record and increments Goal counters in
// the same transaction. Replaying the same (goal_id, run_id, segment_index) is a
// successful no-op that does not change counters. New segments are rejected once
// the Goal is terminal so late retries cannot inflate budgets.
func (r GoalRepository) RecordSegment(goalID, runID string, segmentIndex, delta int) (bool, error) {
	if strings.TrimSpace(goalID) == "" || strings.TrimSpace(runID) == "" || segmentIndex < 0 || delta < 0 {
		return false, fmt.Errorf("invalid goal segment")
	}
	recorded := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.GoalSegment
		findErr := tx.Where("goal_id = ? AND run_id = ? AND segment_index = ?", goalID, runID, segmentIndex).
			First(&existing).Error
		if findErr == nil {
			// Idempotent replay.
			return nil
		}
		if findErr != gorm.ErrRecordNotFound {
			return findErr
		}

		var goal model.Goal
		if err := tx.First(&goal, "id = ?", goalID).Error; err != nil {
			return err
		}
		switch goal.Status {
		case "succeeded", "failed", "cancelled":
			return fmt.Errorf("goal is terminal; cannot record new segments")
		}

		row := model.GoalSegment{
			ID:           fmt.Sprintf("goal_segment_%s_%s_%d", goalID, runID, segmentIndex),
			GoalID:       goalID,
			RunID:        runID,
			SegmentIndex: segmentIndex,
			ToolTurns:    delta,
			CreatedAt:    time.Now().UTC(),
		}
		if err := tx.Create(&row).Error; err != nil {
			// Unique race: another writer inserted the same segment key.
			var again model.GoalSegment
			if tx.Where("goal_id = ? AND run_id = ? AND segment_index = ?", goalID, runID, segmentIndex).
				First(&again).Error == nil {
				return nil
			}
			return err
		}
		recorded = true
		return tx.Model(&model.Goal{}).Where("id = ?", goalID).Updates(map[string]any{
			"used_tool_turns": gorm.Expr("used_tool_turns + ?", delta),
			"used_segments":   gorm.Expr("used_segments + 1"),
			"updated_at":      time.Now().UTC(),
		}).Error
	})
	return recorded, err
}

// CountSegmentsForRun returns how many ledger rows exist for a goal run pair.
func (r GoalRepository) CountSegmentsForRun(goalID, runID string) (int64, error) {
	var n int64
	err := r.db.Model(&model.GoalSegment{}).
		Where("goal_id = ? AND run_id = ?", goalID, runID).
		Count(&n).Error
	return n, err
}

func (r GoalRepository) ListActions(goalID string) ([]model.GoalAction, error) {
	var rows []model.GoalAction
	err := r.db.Where("goal_id = ?", goalID).
		Order("sort_order asc, created_at asc").
		Find(&rows).Error
	return rows, err
}

func (r GoalRepository) ReplaceActions(goal model.Goal, actions []model.GoalAction) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("goal_id = ?", goal.ID).Delete(&model.GoalAction{}).Error; err != nil {
			return err
		}
		for i := range actions {
			actions[i].GoalID = goal.ID
			actions[i].SessionID = goal.SessionID
			if err := tx.Create(&actions[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r GoalRepository) UpdateAction(action model.GoalAction) error {
	action.UpdatedAt = time.Now().UTC()
	return r.db.Model(&model.GoalAction{}).
		Where("id = ? AND goal_id = ?", action.ID, action.GoalID).
		Updates(map[string]any{
			"title": action.Title, "description": action.Description,
			"acceptance": action.Acceptance, "status": action.Status,
			"result": action.Result, "evidence": action.Evidence,
			"attempt": action.Attempt, "sort_order": action.SortOrder,
			"finished_at": action.FinishedAt, "updated_at": action.UpdatedAt,
		}).Error
}

func (r GoalRepository) AppendEvent(event model.GoalEvent) (model.GoalEvent, error) {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var maxSeq int
		if err := tx.Model(&model.GoalEvent{}).
			Where("goal_id = ?", event.GoalID).
			Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
			return err
		}
		event.Seq = maxSeq + 1
		if event.ID == "" {
			event.ID = fmt.Sprintf("goal_event_%s_%d", event.GoalID, event.Seq)
		}
		event.CreatedAt = time.Now().UTC()
		return tx.Create(&event).Error
	})
	return event, err
}

func (r GoalRepository) ListEvents(goalID string, limit int) ([]model.GoalEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.GoalEvent
	err := r.db.Where("goal_id = ?", goalID).
		Order("seq desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// TransitionStatus applies a terminal/pause transition only from one of the
// expected states and, when provided, only for the run currently owning it.
func (r GoalRepository) TransitionStatus(id, sessionID string, from []string, activeRunID string, updates map[string]any) (bool, error) {
	q := r.db.Model(&model.Goal{}).Where("id = ? AND session_id = ? AND status IN ?", id, sessionID, from)
	if activeRunID != "" {
		q = q.Where("active_run_id = ?", activeRunID)
	}
	updates["updated_at"] = time.Now().UTC()
	res := q.Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r GoalRepository) DeleteBySession(sessionID string) error {
	return r.db.Where("session_id = ?", sessionID).Delete(&model.Goal{}).Error
}

func (r GoalRepository) CancelActiveBySessions(sessionIDs []string, reason string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	res := r.db.Model(&model.Goal{}).
		Where("session_id IN ? AND status IN ?", sessionIDs, []string{"pending", "active", "paused"}).
		Updates(map[string]any{
			"status": "cancelled", "pause_reason": reason, "active_run_id": "",
			"finished_at": now, "updated_at": now,
		})
	return res.RowsAffected, res.Error
}

// CalculateUsedWallTime derives usage from finished bound runs, making repeated
// finish-event handling idempotent.
func (r GoalRepository) CalculateUsedWallTime(goalID string) (int, error) {
	var runs []model.RunRecord
	if err := r.db.Where("goal_id = ? AND finished_at IS NOT NULL", goalID).Find(&runs).Error; err != nil {
		return 0, err
	}
	total := time.Duration(0)
	for _, run := range runs {
		if run.FinishedAt == nil || run.FinishedAt.Before(run.StartedAt) {
			continue
		}
		total += run.FinishedAt.Sub(run.StartedAt)
	}
	return int(total / time.Second), nil
}

func applyGoalBudgetDefaults(row *model.Goal) {
	if row.MaxSegmentsPerRun <= 0 {
		row.MaxSegmentsPerRun = 4
	}
	if row.MaxToolTurnsPerSegment <= 0 {
		row.MaxToolTurnsPerSegment = 12
	}
	if row.MaxTotalToolTurns <= 0 {
		row.MaxTotalToolTurns = 96
	}
	if row.MaxWallTimeSec <= 0 {
		row.MaxWallTimeSec = 1800
	}
	if row.MaxIterations <= 0 {
		row.MaxIterations = 20
	} else if row.MaxIterations > 100 {
		row.MaxIterations = 100
	}
	if row.MaxStagnation <= 0 {
		row.MaxStagnation = 3
	} else if row.MaxStagnation > 10 {
		row.MaxStagnation = 10
	}
	if row.Version <= 0 {
		row.Version = 1
	}
}
