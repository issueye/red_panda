package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

type ProviderProfileRepository struct {
	db *gorm.DB
}

func NewProviderProfileRepository(db *gorm.DB) ProviderProfileRepository {
	return ProviderProfileRepository{db: db}
}

func (r ProviderProfileRepository) Create(profile model.ProviderProfile) (model.ProviderProfile, error) {
	now := time.Now().UTC()
	if profile.ID == "" {
		profile.ID = fmt.Sprintf("provider_%d", now.UnixNano())
	}
	if profile.Provider == "" {
		profile.Provider = "openai_compatible"
	}
	if profile.Name == "" {
		profile.Name = profile.Provider
	}
	profile.Active = true
	profile.CreatedAt = now
	profile.UpdatedAt = now

	err := r.db.Transaction(func(tx *gorm.DB) error {
		if profile.IsDefault {
			if err := tx.Model(&model.ProviderProfile{}).
				Where("deleted_at IS NULL").
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(&profile).Error
	})
	return profile, err
}

func (r ProviderProfileRepository) Update(profile model.ProviderProfile) (model.ProviderProfile, error) {
	var current model.ProviderProfile
	if err := r.db.First(&current, "id = ? AND deleted_at IS NULL", profile.ID).Error; err != nil {
		return model.ProviderProfile{}, err
	}
	if profile.Name != "" {
		current.Name = profile.Name
	}
	if profile.Provider != "" {
		current.Provider = profile.Provider
	}
	if profile.BaseURL != "" {
		current.BaseURL = profile.BaseURL
	}
	if profile.Model != "" {
		current.Model = profile.Model
	}
	// MaxTokens is intentionally always applied so callers can clear it to 0.
	current.MaxTokens = profile.MaxTokens
	if profile.APIKeySecret != "" {
		current.APIKeySecret = profile.APIKeySecret
	}
	current.IsDefault = profile.IsDefault
	current.Stream = profile.Stream
	current.Active = profile.Active
	current.UpdatedAt = time.Now().UTC()

	err := r.db.Transaction(func(tx *gorm.DB) error {
		if current.IsDefault {
			if err := tx.Model(&model.ProviderProfile{}).
				Where("id <> ? AND deleted_at IS NULL", current.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Save(&current).Error
	})
	return current, err
}

func (r ProviderProfileRepository) Get(id string) (model.ProviderProfile, error) {
	var row model.ProviderProfile
	err := r.db.First(&row, "id = ? AND deleted_at IS NULL", id).Error
	return row, err
}

func (r ProviderProfileRepository) Default() (model.ProviderProfile, error) {
	var row model.ProviderProfile
	err := r.db.First(&row, "is_default = ? AND active = ? AND deleted_at IS NULL", true, true).Error
	return row, err
}

func (r ProviderProfileRepository) List(limit int) ([]model.ProviderProfile, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []model.ProviderProfile
	err := r.db.
		Where("deleted_at IS NULL").
		Order("is_default desc, updated_at desc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r ProviderProfileRepository) Delete(id string) error {
	now := time.Now().UTC()
	return r.db.Model(&model.ProviderProfile{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{
			"active":     false,
			"is_default": false,
			"deleted_at": &now,
			"updated_at": now,
		}).Error
}
