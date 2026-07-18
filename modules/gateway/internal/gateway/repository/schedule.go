package repository

import (
	"fmt"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

var (
	scheduleIDCounter    atomic.Uint64
	scheduleRunIDCounter atomic.Uint64
)

type ScheduleRepository struct {
	db *gorm.DB
}

func NewScheduleRepository(db *gorm.DB) ScheduleRepository {
	return ScheduleRepository{db: db}
}

func (r ScheduleRepository) Create(row model.ScheduledTask) (model.ScheduledTask, error) {
	now := time.Now().UTC()
	if row.ID == "" {
		row.ID = newScheduleID(now)
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	return row, r.db.Create(&row).Error
}

func (r ScheduleRepository) Update(row model.ScheduledTask) (model.ScheduledTask, error) {
	row.UpdatedAt = time.Now().UTC()
	return row, r.db.Save(&row).Error
}

func (r ScheduleRepository) Get(id string) (model.ScheduledTask, error) {
	var row model.ScheduledTask
	err := r.db.Where("id = ? AND deleted_at IS NULL", id).First(&row).Error
	return row, err
}

func (r ScheduleRepository) GetByName(name string) (model.ScheduledTask, error) {
	var row model.ScheduledTask
	err := r.db.Where("name = ? AND deleted_at IS NULL", name).First(&row).Error
	return row, err
}

func (r ScheduleRepository) List(enabledOnly bool, limit int) ([]model.ScheduledTask, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := r.db.Where("deleted_at IS NULL")
	if enabledOnly {
		q = q.Where("enabled = ?", true)
	}
	var rows []model.ScheduledTask
	err := q.Order("updated_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r ScheduleRepository) DisableFixedForSessions(sessionIDs []string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}
	res := r.db.Model(&model.ScheduledTask{}).
		Where("session_id IN ? AND session_mode = ? AND enabled = ? AND deleted_at IS NULL",
			sessionIDs, "fixed_session", true).
		Updates(map[string]any{"enabled": false, "next_run_at": nil, "updated_at": time.Now().UTC()})
	return res.RowsAffected, res.Error
}

func (r ScheduleRepository) SoftDelete(id string) (model.ScheduledTask, error) {
	row, err := r.Get(id)
	if err != nil {
		return model.ScheduledTask{}, err
	}
	now := time.Now().UTC()
	row.Enabled = false
	row.DeletedAt = &now
	row.NextRunAt = nil
	row.UpdatedAt = now
	return row, r.db.Save(&row).Error
}

// ListDue returns enabled tasks whose next_run_at is at or before now.
func (r ScheduleRepository) ListDue(now time.Time, limit int) ([]model.ScheduledTask, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows []model.ScheduledTask
	err := r.db.Where("deleted_at IS NULL AND enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now.UTC()).
		Order("next_run_at asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// ListEnabledWithPastNext returns enabled tasks with a past next_run_at (startup catch-up).
func (r ScheduleRepository) ListEnabledWithPastNext(now time.Time) ([]model.ScheduledTask, error) {
	var rows []model.ScheduledTask
	err := r.db.Where("deleted_at IS NULL AND enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now.UTC()).
		Order("next_run_at asc").
		Find(&rows).Error
	return rows, err
}

func (r ScheduleRepository) CreateRun(row model.ScheduledTaskRun) (model.ScheduledTaskRun, error) {
	now := time.Now().UTC()
	if row.ID == "" {
		row.ID = newScheduleRunID(now)
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	return row, r.db.Create(&row).Error
}

func (r ScheduleRepository) UpdateRun(row model.ScheduledTaskRun) error {
	return r.db.Save(&row).Error
}

func (r ScheduleRepository) GetRun(id string) (model.ScheduledTaskRun, error) {
	var row model.ScheduledTaskRun
	err := r.db.Where("id = ?", id).First(&row).Error
	return row, err
}

func (r ScheduleRepository) GetRunByRunID(runID string) (model.ScheduledTaskRun, error) {
	var row model.ScheduledTaskRun
	err := r.db.Where("run_id = ?", runID).First(&row).Error
	return row, err
}

func (r ScheduleRepository) HasOpenRun(scheduleID string) (bool, error) {
	var count int64
	err := r.db.Model(&model.ScheduledTaskRun{}).
		Where("schedule_id = ? AND status IN ?", scheduleID, []string{"starting", "running"}).
		Count(&count).Error
	return count > 0, err
}

func (r ScheduleRepository) ListRuns(scheduleID string, limit int) ([]model.ScheduledTaskRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.ScheduledTaskRun
	err := r.db.Where("schedule_id = ?", scheduleID).Order("created_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// Transaction exposes the DB for claim transactions.
func (r ScheduleRepository) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

func (r ScheduleRepository) DB() *gorm.DB {
	return r.db
}

func newScheduleID(now time.Time) string {
	return fmt.Sprintf("sched_%d_%d", now.UnixNano(), scheduleIDCounter.Add(1))
}

func newScheduleRunID(now time.Time) string {
	return fmt.Sprintf("srun_%d_%d", now.UnixNano(), scheduleRunIDCounter.Add(1))
}

// NewScheduleID exports id generation for services.
func NewScheduleID() string {
	return newScheduleID(time.Now().UTC())
}

// NewScheduleRunID exports schedule-run id generation.
func NewScheduleRunID() string {
	return newScheduleRunID(time.Now().UTC())
}
