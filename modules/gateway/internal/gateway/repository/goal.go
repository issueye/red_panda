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
	if row.PipelinePhase == "" {
		row.PipelinePhase = "analyze"
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

// CASStatus updates only when current status matches fromStatus.
func (r GoalRepository) CASStatus(id string, fromStatus string, patch model.Goal) (bool, error) {
	patch.UpdatedAt = time.Now().UTC()
	updates := map[string]any{
		"status":             patch.Status,
		"pipeline_phase":     patch.PipelinePhase,
		"pause_reason":       patch.PauseReason,
		"fail_reason":        patch.FailReason,
		"analysis_summary":   patch.AnalysisSummary,
		"checkpoint_summary": patch.CheckpointSummary,
		"progress_note":      patch.ProgressNote,
		"report_json":        patch.ReportJSON,
		"report_markdown":    patch.ReportMarkdown,
		"active_run_id":      patch.ActiveRunID,
		"last_run_id":        patch.LastRunID,
		"used_tool_turns":    patch.UsedToolTurns,
		"used_segments":      patch.UsedSegments,
		"used_wall_time_sec": patch.UsedWallTimeSec,
		"updated_at":         patch.UpdatedAt,
		"finished_at":        patch.FinishedAt,
		"started_at":         patch.StartedAt,
		"title":              patch.Title,
		"objective":          patch.Objective,
		"success_criteria":   patch.SuccessCriteria,
	}
	res := r.db.Model(&model.Goal{}).
		Where("id = ? AND status = ?", id, fromStatus).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r GoalRepository) AddToolTurns(id string, delta int) error {
	if delta <= 0 {
		return nil
	}
	return r.db.Model(&model.Goal{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"used_tool_turns": gorm.Expr("used_tool_turns + ?", delta),
			"used_segments":   gorm.Expr("used_segments + ?", 1),
			"updated_at":      time.Now().UTC(),
		}).Error
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
}
