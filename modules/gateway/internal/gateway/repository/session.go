package repository

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

type SessionRepository struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) SessionRepository {
	return SessionRepository{db: db}
}

func (r SessionRepository) Create(name string, workspaceRoot string) (model.Session, error) {
	now := time.Now().UTC()
	session := model.Session{
		ID:            fmt.Sprintf("session_%d", now.UnixNano()),
		Name:          name,
		WorkspaceRoot: workspaceRoot,
		Status:        "active",
		Kind:          "normal",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if session.Name == "" {
		session.Name = "New session"
	}
	return session, r.db.Create(&session).Error
}

func (r SessionRepository) Ensure(id string, name string, workspaceRoot string) (model.Session, error) {
	if id == "" {
		return r.Create(name, workspaceRoot)
	}
	var session model.Session
	err := r.db.Where("id = ?", id).First(&session).Error
	if err == nil {
		return session, nil
	}
	if err != gorm.ErrRecordNotFound {
		return model.Session{}, err
	}
	now := time.Now().UTC()
	session = model.Session{
		ID:            id,
		Name:          name,
		WorkspaceRoot: workspaceRoot,
		Status:        "active",
		Kind:          "normal",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if session.Name == "" {
		session.Name = id
	}
	return session, r.db.Create(&session).Error
}

func (r SessionRepository) Get(id string) (model.Session, error) {
	var session model.Session
	err := r.db.Where("id = ? AND deleted_at IS NULL", id).First(&session).Error
	return session, err
}

func (r SessionRepository) CreateDerived(name string, source model.Session, kind string, forkPointSeq uint64, forkPointRunID string) (model.Session, error) {
	now := time.Now().UTC()
	session := model.Session{
		ID:              fmt.Sprintf("session_%d", now.UnixNano()),
		Name:            name,
		WorkspaceRoot:   source.WorkspaceRoot,
		Status:          "active",
		ParentID:        source.ID,
		Kind:            kind,
		SourceSessionID: source.ID,
		ForkPointSeq:    forkPointSeq,
		ForkPointRunID:  forkPointRunID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if session.Name == "" {
		switch kind {
		case "compact":
			session.Name = source.Name + " compact"
		case "fork":
			session.Name = source.Name + " fork"
		default:
			session.Name = source.Name
		}
	}
	return session, r.db.Create(&session).Error
}

func (r SessionRepository) List(limit int) ([]model.Session, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []model.Session
	err := r.db.Where("deleted_at IS NULL").Order("updated_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r SessionRepository) ListAll() ([]model.Session, error) {
	var rows []model.Session
	err := r.db.Where("deleted_at IS NULL").Order("updated_at desc, id desc").Find(&rows).Error
	return rows, err
}

func (r SessionRepository) ListPage(offset int, limit int) ([]model.Session, bool, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	} else if limit > 200 {
		limit = 200
	}
	var rows []model.Session
	err := r.db.Where("deleted_at IS NULL").Order("updated_at desc, id desc").
		Offset(offset).Limit(limit + 1).Find(&rows).Error
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return rows, hasMore, nil
}

func (r SessionRepository) ListByWorkspace(workspaceRoot string) ([]model.Session, error) {
	var rows []model.Session
	err := r.db.Where("workspace_root = ? AND deleted_at IS NULL", workspaceRoot).
		Order("updated_at desc").Find(&rows).Error
	return rows, err
}

func (r SessionRepository) Touch(id string) error {
	return r.db.Model(&model.Session{}).Where("id = ?", id).Updates(map[string]any{
		"updated_at": time.Now().UTC(),
	}).Error
}

// SoftDelete marks a session as deleted so it disappears from list/bootstrap.
func (r SessionRepository) SoftDelete(id string) error {
	now := time.Now().UTC()
	result := r.db.Model(&model.Session{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{
			"status":     "deleted",
			"deleted_at": now,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r SessionRepository) SoftDeleteMany(sessionIDs []string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	result := r.db.Model(&model.Session{}).
		Where("id IN ? AND deleted_at IS NULL", sessionIDs).
		Updates(map[string]any{"status": "deleted", "deleted_at": now, "updated_at": now})
	return result.RowsAffected, result.Error
}

func stableID(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:8])
}
