package repository

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"redpanda/gateway/internal/gateway/model"
)

var workerProfileBuiltinSeedMu sync.Mutex

type WorkerProfileRepository struct {
	db *gorm.DB
}

func NewWorkerProfileRepository(db *gorm.DB) WorkerProfileRepository {
	return WorkerProfileRepository{db: db}
}

func (r WorkerProfileRepository) Create(row model.WorkerProfile) (model.WorkerProfile, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(row.ID) == "" {
		row.ID = fmt.Sprintf("worker_profile_%d", now.UnixNano())
	}
	row.Key = strings.TrimSpace(row.Key)
	if row.Key == "" {
		return model.WorkerProfile{}, fmt.Errorf("key is required")
	}
	if strings.TrimSpace(row.Name) == "" {
		row.Name = row.Key
	}
	if row.Kind == "" {
		row.Kind = "custom"
	}
	if row.Phase == "" {
		row.Phase = "custom"
	}
	row.CreatedAt = now
	row.UpdatedAt = now
	row.DeletedAt = nil
	if err := r.db.Create(&row).Error; err != nil {
		return model.WorkerProfile{}, err
	}
	return row, nil
}

// UpsertBuiltin creates a builtin profile or refreshes only its immutable
// identity fields. Operator-managed execution settings survive every reseed.
func (r WorkerProfileRepository) UpsertBuiltin(row model.WorkerProfile) (model.WorkerProfile, error) {
	workerProfileBuiltinSeedMu.Lock()
	defer workerProfileBuiltinSeedMu.Unlock()

	row.Key = strings.TrimSpace(row.Key)
	if row.Key == "" {
		return model.WorkerProfile{}, fmt.Errorf("key is required")
	}
	row.Kind = "builtin"
	row.Builtin = true
	row.ToolDenylist = enforceBuiltinWorkerDenylist(row.ToolDenylist)
	var current model.WorkerProfile
	err := r.db.Where("key = ? AND deleted_at IS NULL", row.Key).First(&current).Error
	if err == gorm.ErrRecordNotFound {
		now := time.Now().UTC()
		if strings.TrimSpace(row.ID) == "" {
			row.ID = fmt.Sprintf("worker_profile_%d", now.UnixNano())
		}
		if strings.TrimSpace(row.Name) == "" {
			row.Name = row.Key
		}
		row.CreatedAt = now
		row.UpdatedAt = now
		row.DeletedAt = nil
		result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return model.WorkerProfile{}, result.Error
		}
		if result.RowsAffected != 0 {
			return row, nil
		}
		if err := r.db.Where("key = ? AND deleted_at IS NULL", row.Key).First(&current).Error; err != nil {
			return model.WorkerProfile{}, err
		}
		if !current.Builtin {
			return model.WorkerProfile{}, fmt.Errorf("builtin key %q is used by a custom worker profile", row.Key)
		}
		err = nil
	}
	if err != nil {
		return model.WorkerProfile{}, err
	}
	if !current.Builtin {
		return model.WorkerProfile{}, fmt.Errorf("builtin key %q is used by a custom worker profile", row.Key)
	}
	current.Key = row.Key
	current.Kind = "builtin"
	current.Phase = row.Phase
	current.Builtin = true
	current.ToolDenylist = enforceBuiltinWorkerDenylist(current.ToolDenylist)
	current.UpdatedAt = time.Now().UTC()
	if err := r.db.Save(&current).Error; err != nil {
		return model.WorkerProfile{}, err
	}
	return current, nil
}

func (r WorkerProfileRepository) Update(row model.WorkerProfile) (model.WorkerProfile, error) {
	var current model.WorkerProfile
	if err := r.db.First(&current, "id = ? AND deleted_at IS NULL", row.ID).Error; err != nil {
		return model.WorkerProfile{}, err
	}
	current.Name = row.Name
	current.NameZH = row.NameZH
	current.Phase = row.Phase
	current.Description = row.Description
	current.SystemPrompt = row.SystemPrompt
	current.Provider = row.Provider
	current.Model = row.Model
	current.ToolAllowlist = row.ToolAllowlist
	current.ToolDenylist = row.ToolDenylist
	if current.Builtin {
		current.ToolDenylist = enforceBuiltinWorkerDenylist(current.ToolDenylist)
	}
	current.DefaultMaxTurns = row.DefaultMaxTurns
	current.Enabled = row.Enabled
	current.SortOrder = row.SortOrder
	current.MetadataJSON = row.MetadataJSON
	current.UpdatedAt = time.Now().UTC()
	if err := r.db.Save(&current).Error; err != nil {
		return model.WorkerProfile{}, err
	}
	return current, nil
}

func enforceBuiltinWorkerDenylist(items []string) []string {
	out := make([]string, 0, len(items)+1)
	seen := make(map[string]struct{}, len(items)+1)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || strings.HasPrefix(item, "subagent.") {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if _, exists := seen["worker.delegate"]; !exists {
		out = append(out, "worker.delegate")
	}
	return out
}

func (r WorkerProfileRepository) Get(id string) (model.WorkerProfile, error) {
	var row model.WorkerProfile
	err := r.db.First(&row, "id = ? AND deleted_at IS NULL", id).Error
	return row, err
}

func (r WorkerProfileRepository) GetByKey(key string) (model.WorkerProfile, error) {
	var row model.WorkerProfile
	err := r.db.First(&row, "key = ? AND deleted_at IS NULL", strings.TrimSpace(key)).Error
	return row, err
}

func (r WorkerProfileRepository) List(limit int) ([]model.WorkerProfile, error) {
	return r.list(limit, false)
}

func (r WorkerProfileRepository) ListEnabled(limit int) ([]model.WorkerProfile, error) {
	return r.list(limit, true)
}

func (r WorkerProfileRepository) list(limit int, enabledOnly bool) ([]model.WorkerProfile, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := r.db.Where("deleted_at IS NULL")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	var rows []model.WorkerProfile
	err := query.Order("sort_order asc, created_at asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r WorkerProfileRepository) SoftDelete(id string) error {
	var current model.WorkerProfile
	if err := r.db.First(&current, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
		return err
	}
	if current.Builtin {
		return fmt.Errorf("builtin worker profile %q cannot be deleted", current.Key)
	}
	now := time.Now().UTC()
	return r.db.Model(&model.WorkerProfile{}).Where("id = ?", id).Updates(map[string]any{
		"deleted_at": now,
		"updated_at": now,
	}).Error
}
