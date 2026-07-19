package repository

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

// ContextRepository persists GoalNote scratchpad entries that are shared across
// segments, runs, and specialist workers for a goal.
type ContextRepository struct {
	db *gorm.DB
}

func NewContextRepository(db *gorm.DB) ContextRepository {
	return ContextRepository{db: db}
}

// NoteListOpts filters the scratchpad view of a goal.
type NoteListOpts struct {
	Kind     string
	Pinned   *bool
	Limit    int
	SinceSeq int
}

// Append creates a new note with a goal-scoped monotonic sequence number.
// The ToolCallID (carried via the ID field when set) makes appends idempotent:
// re-appending the same ID returns the existing row without creating a duplicate.
func (r ContextRepository) Append(row model.GoalNote) (model.GoalNote, bool, error) {
	now := time.Now().UTC()
	row.CreatedAt = now
	row.UpdatedAt = now
	if strings.TrimSpace(row.ID) == "" {
		row.ID = fmt.Sprintf("note_%d_%s", now.UnixNano(), shortID(row.GoalID))
	}
	if strings.TrimSpace(row.Kind) == "" {
		row.Kind = "finding"
	}
	recorded := true
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// Idempotency: if the ID already exists for this goal, return the existing row.
		var existing model.GoalNote
		if err := tx.Where("id = ? AND goal_id = ?", row.ID, row.GoalID).First(&existing).Error; err == nil {
			row = existing
			recorded = false
			return nil
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		var maxSeq *int
		if err := tx.Model(&model.GoalNote{}).Where("goal_id = ?", row.GoalID).
			Select("COALESCE(MAX(seq), 0)").Row().Scan(&maxSeq); err != nil {
			return err
		}
		if maxSeq == nil {
			row.Seq = 1
		} else {
			row.Seq = *maxSeq + 1
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return model.GoalNote{}, false, err
	}
	return row, recorded, nil
}

// Replace upserts a note keyed by (goal_id, kind, title). When a matching row
// exists its body and metadata are updated; otherwise a new note is appended.
func (r ContextRepository) Replace(row model.GoalNote) (model.GoalNote, bool, error) {
	now := time.Now().UTC()
	row.UpdatedAt = now
	if strings.TrimSpace(row.Kind) == "" {
		row.Kind = "finding"
	}
	created := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.GoalNote
		err := tx.Where("goal_id = ? AND kind = ? AND title = ?", row.GoalID, row.Kind, row.Title).
			First(&existing).Error
		if err == nil {
			existing.Body = row.Body
			existing.Phase = row.Phase
			existing.Source = row.Source
			existing.RunID = row.RunID
			existing.Pinned = row.Pinned
			existing.UpdatedAt = now
			row = existing
			return tx.Save(&existing).Error
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		created = true
		row.CreatedAt = now
		if strings.TrimSpace(row.ID) == "" {
			row.ID = fmt.Sprintf("note_%d_%s", now.UnixNano(), shortID(row.GoalID))
		}
		var maxSeq *int
		if err := tx.Model(&model.GoalNote{}).Where("goal_id = ?", row.GoalID).
			Select("COALESCE(MAX(seq), 0)").Row().Scan(&maxSeq); err != nil {
			return err
		}
		if maxSeq == nil {
			row.Seq = 1
		} else {
			row.Seq = *maxSeq + 1
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return model.GoalNote{}, false, err
	}
	return row, created, nil
}

func (r ContextRepository) Get(goalID string, noteID string) (model.GoalNote, error) {
	var row model.GoalNote
	err := r.db.Where("id = ? AND goal_id = ?", noteID, goalID).First(&row).Error
	return row, err
}

// List returns notes for a goal, pinned first then by descending sequence,
// applying the optional kind / pinned / since-seq filters.
func (r ContextRepository) List(goalID string, opts NoteListOpts) ([]model.GoalNote, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 50
	}
	q := r.db.Where("goal_id = ?", goalID)
	if strings.TrimSpace(opts.Kind) != "" {
		q = q.Where("kind = ?", opts.Kind)
	}
	if opts.Pinned != nil {
		q = q.Where("pinned = ?", *opts.Pinned)
	}
	if opts.SinceSeq > 0 {
		q = q.Where("seq > ?", opts.SinceSeq)
	}
	var rows []model.GoalNote
	err := q.Order("pinned desc, seq desc").Limit(opts.Limit).Find(&rows).Error
	return rows, err
}

// Search performs a case-insensitive LIKE match on title and body.
func (r ContextRepository) Search(goalID string, query string, limit int) ([]model.GoalNote, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	like := "%" + strings.ToLower(query) + "%"
	var rows []model.GoalNote
	err := r.db.Where("goal_id = ? AND (LOWER(title) LIKE ? OR LOWER(body) LIKE ?)", goalID, like, like).
		Order("pinned desc, seq desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r ContextRepository) Delete(goalID string, noteID string) error {
	return r.db.Where("id = ? AND goal_id = ?", noteID, goalID).Delete(&model.GoalNote{}).Error
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}
