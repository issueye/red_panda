package repository

import (
	"fmt"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

var todoIDCounter atomic.Uint64

type TodoRepository struct {
	db *gorm.DB
}

func NewTodoRepository(db *gorm.DB) TodoRepository {
	return TodoRepository{db: db}
}

func (r TodoRepository) ListBySession(sessionID string) ([]model.TodoItem, error) {
	var rows []model.TodoItem
	err := r.db.Where("session_id = ?", sessionID).Order("sort_order asc, created_at asc").Find(&rows).Error
	return rows, err
}

func (r TodoRepository) ListOpenBySession(sessionID string) ([]model.TodoItem, error) {
	var rows []model.TodoItem
	err := r.db.Where("session_id = ? AND status IN ?", sessionID, []string{"pending", "in_progress"}).
		Order("sort_order asc, created_at asc").Find(&rows).Error
	return rows, err
}

func (r TodoRepository) ReplaceSession(sessionID string, items []model.TodoItem) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", sessionID).Delete(&model.TodoItem{}).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		return tx.Create(&items).Error
	})
}

func (r TodoRepository) SaveAll(items []model.TodoItem) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for i := range items {
			if items[i].ID == "" {
				if err := tx.Create(&items[i]).Error; err != nil {
					return err
				}
				continue
			}
			var count int64
			if err := tx.Model(&model.TodoItem{}).Where("id = ?", items[i].ID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := tx.Create(&items[i]).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Save(&items[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r TodoRepository) DeleteBySession(sessionID string) error {
	return r.db.Where("session_id = ?", sessionID).Delete(&model.TodoItem{}).Error
}

func (r TodoRepository) DeleteBySessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	return r.db.Where("session_id IN ?", sessionIDs).Delete(&model.TodoItem{}).Error
}

// CopySessionTodos copies todos from source to target with new IDs.
// openOnly keeps pending/in_progress only (compact); false copies all (fork).
func (r TodoRepository) CopySessionTodos(sourceSessionID, targetSessionID string, openOnly bool) (int, error) {
	rows, err := r.ListBySession(sourceSessionID)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	copied := make([]model.TodoItem, 0, len(rows))
	order := 0
	for _, row := range rows {
		if openOnly && row.Status != "pending" && row.Status != "in_progress" {
			continue
		}
		item := row
		item.ID = newTodoID(now)
		item.SessionID = targetSessionID
		item.SortOrder = order
		item.CreatedAt = now
		item.UpdatedAt = now
		if item.Status == "completed" || item.Status == "cancelled" {
			// keep CompletedAt from source if present
		} else {
			item.CompletedAt = nil
		}
		copied = append(copied, item)
		order++
	}
	if len(copied) == 0 {
		return 0, nil
	}
	if err := r.db.Create(&copied).Error; err != nil {
		return 0, err
	}
	return len(copied), nil
}

func newTodoID(now time.Time) string {
	return fmt.Sprintf("todo_%d_%d", now.UnixNano(), todoIDCounter.Add(1))
}

// NewTodoID generates a gateway primary key for a todo row.
func NewTodoID() string {
	return newTodoID(time.Now().UTC())
}
