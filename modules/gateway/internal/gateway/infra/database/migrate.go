package database

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol"
)

func Migrate(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := migrateProviderProfileStream(tx); err != nil {
			return err
		}
		if err := repairLegacyMessageSequences(tx); err != nil {
			return err
		}
		if err := repairLegacyActiveCompactions(tx); err != nil {
			return err
		}
		if err := tx.AutoMigrate(
			&model.Session{},
			&model.SessionLineage{},
			&model.SessionCompaction{},
			&model.MemoryRecord{},
			&model.TodoItem{},
			&model.Message{},
			&model.RunRecord{},
			&model.RunEvent{},
			&model.ToolCall{},
			&model.Workspace{},
			&model.PermissionRequest{},
			&model.ProviderProfile{},
			&model.MCPServerConfig{},
			&model.WorkerProfile{},
			&model.ScheduledTask{},
			&model.ScheduledTaskRun{},
			&model.Attachment{},
		); err != nil {
			return err
		}
		if err := migrateWorkerProfiles(tx); err != nil {
			return err
		}
		if err := migrateProviderProfileSupportsVision(tx); err != nil {
			return err
		}
		if err := migrateProviderProfileHTTPProxy(tx); err != nil {
			return err
		}
		if err := migratePromptCacheMetadata(tx); err != nil {
			return err
		}
		// Goal feature removed (docs/37, docs/31-34): drop legacy tables + column
		// left behind by prior migrations. Idempotent; no-op on fresh databases.
		if err := dropGoalLegacySchema(tx); err != nil {
			return err
		}
		return tx.Migrator().DropTable("agent_definitions")
	})
}

func migratePromptCacheMetadata(db *gorm.DB) error {
	if db.Migrator().HasTable(&model.ProviderProfile{}) {
		if err := db.Model(&model.ProviderProfile{}).
			Where("cache_mode IS NULL OR TRIM(cache_mode) = ''").
			Update("cache_mode", gorm.Expr("CASE WHEN LOWER(provider) = ? THEN ? ELSE ? END", "anthropic", "explicit", "implicit")).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasTable(&model.SessionCompaction{}) {
		return nil
	}
	var rows []model.SessionCompaction
	if err := db.Where("summary_json <> '' AND (summary_digest = '' OR prompt_schema_version = '' OR cache_epoch = '')").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		summaryDigest := migrationDigestHex([]byte(row.SummaryJSON))
		cacheEpoch := migrationDigestHex([]byte(fmt.Sprintf("compaction:%s:%s:%d:%d", row.SourceSessionID, summaryDigest, row.SourceStartSeq, row.SourceEndSeq)))
		if err := db.Model(&model.SessionCompaction{}).Where("id = ?", row.ID).Updates(map[string]any{
			"summary_digest":        summaryDigest,
			"prompt_schema_version": protocol.PromptSchemaVersion,
			"cache_epoch":           cacheEpoch,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrationDigestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// dropGoalLegacySchema removes the Goal feature tables and the RunRecord.GoalID
// column that older databases still carry. Safe to run on every boot: each step
// checks for existence before dropping.
func dropGoalLegacySchema(tx *gorm.DB) error {
	for _, table := range []string{
		"goals", "goal_segments", "goal_notes", "goal_actions", "goal_events",
	} {
		if tx.Migrator().HasTable(table) {
			if err := tx.Migrator().DropTable(table); err != nil {
				return err
			}
		}
	}
	if tx.Migrator().HasColumn(&model.RunRecord{}, "GoalID") {
		if err := tx.Migrator().DropColumn(&model.RunRecord{}, "GoalID"); err != nil {
			return err
		}
	}
	return nil
}

func repairLegacyActiveCompactions(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.SessionCompaction{}) ||
		db.Migrator().HasIndex(&model.SessionCompaction{}, "idx_compactions_one_applied") {
		return nil
	}
	var rows []model.SessionCompaction
	if err := db.Where("status = ?", "applied").
		Order("source_session_id asc, target_session_id asc, created_at desc, id desc").Find(&rows).Error; err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		key := row.SourceSessionID + "\x00" + row.TargetSessionID
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			continue
		}
		if err := db.Model(&model.SessionCompaction{}).Where("id = ?", row.ID).
			Updates(map[string]any{"status": "superseded", "updated_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
	}
	return nil
}

// repairLegacyMessageSequences runs before AutoMigrate adds the unique
// (session_id, seq) index. Old databases could contain duplicate sequences.
func repairLegacyMessageSequences(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.Message{}) || db.Migrator().HasIndex(&model.Message{}, "idx_messages_session_seq") {
		return nil
	}
	var duplicateGroups int64
	if err := db.Raw(`SELECT COUNT(*) FROM (
		SELECT session_id, seq FROM messages GROUP BY session_id, seq HAVING COUNT(*) > 1
	)`).Scan(&duplicateGroups).Error; err != nil {
		return err
	}
	if duplicateGroups == 0 {
		return nil
	}
	var rows []model.Message
	if err := db.Order("session_id asc, seq asc, created_at asc, id asc").Find(&rows).Error; err != nil {
		return err
	}
	currentSession := ""
	var seq uint64
	for _, row := range rows {
		if row.SessionID != currentSession {
			currentSession = row.SessionID
			seq = 0
		}
		seq++
		if row.Seq == seq {
			continue
		}
		if err := db.Model(&model.Message{}).Where("id = ?", row.ID).Update("seq", seq).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateProviderProfileStream(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.ProviderProfile{}) ||
		db.Migrator().HasColumn(&model.ProviderProfile{}, "stream") {
		return nil
	}
	// Profiles created before the setting existed used the application's
	// streaming default, so preserve that behavior when adding the column.
	return db.Exec("ALTER TABLE provider_profiles ADD COLUMN stream numeric NOT NULL DEFAULT 1").Error
}

// migrateProviderProfileSupportsVision adds the supports_vision column to
// legacy provider_profiles rows. New profiles default to vision off to avoid
// silently routing images to models that cannot handle them (docs/51 §6.5).
func migrateProviderProfileSupportsVision(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.ProviderProfile{}) ||
		db.Migrator().HasColumn(&model.ProviderProfile{}, "supports_vision") {
		return nil
	}
	return db.Exec("ALTER TABLE provider_profiles ADD COLUMN supports_vision numeric NOT NULL DEFAULT 0").Error
}

// migrateProviderProfileHTTPProxy adds the http_proxy column to legacy
// provider_profiles rows. New profiles default to empty (use environment
// proxy). Nullable text — no default needed.
func migrateProviderProfileHTTPProxy(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.ProviderProfile{}) ||
		db.Migrator().HasColumn(&model.ProviderProfile{}, "http_proxy") {
		return nil
	}
	return db.Exec("ALTER TABLE provider_profiles ADD COLUMN http_proxy text").Error
}

type legacyAgentDefinition struct {
	ID              string `gorm:"primaryKey"`
	Key             string
	Name            string
	NameZH          string
	Kind            string
	Phase           string
	Description     string
	SystemPrompt    string
	ToolAllowlist   []string `gorm:"column:tool_allowlist_json;serializer:json;type:text"`
	ToolDenylist    []string `gorm:"column:tool_denylist_json;serializer:json;type:text"`
	DefaultMaxTurns int
	Enabled         bool
	Builtin         bool
	SortOrder       int
	MetadataJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

func (legacyAgentDefinition) TableName() string { return "agent_definitions" }

func migrateWorkerProfiles(db *gorm.DB) error {
	if !db.Migrator().HasTable("agent_definitions") {
		return nil
	}
	var definitions []legacyAgentDefinition
	if err := db.Where("deleted_at IS NULL").
		Order("created_at asc, id asc").
		Find(&definitions).Error; err != nil {
		return err
	}
	for _, definition := range definitions {
		var count int64
		if err := db.Model(&model.WorkerProfile{}).
			Where("key = ? AND deleted_at IS NULL", definition.Key).
			Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			continue
		}
		profileID, err := nextWorkerProfileID(db, definition.ID)
		if err != nil {
			return err
		}
		profile := model.WorkerProfile{
			ID:              profileID,
			Key:             definition.Key,
			Name:            definition.Name,
			NameZH:          definition.NameZH,
			Kind:            definition.Kind,
			Phase:           definition.Phase,
			Description:     definition.Description,
			SystemPrompt:    definition.SystemPrompt,
			ToolAllowlist:   definition.ToolAllowlist,
			ToolDenylist:    definition.ToolDenylist,
			DefaultMaxTurns: definition.DefaultMaxTurns,
			Enabled:         definition.Enabled,
			Builtin:         definition.Builtin,
			SortOrder:       definition.SortOrder,
			MetadataJSON:    definition.MetadataJSON,
			CreatedAt:       definition.CreatedAt,
			UpdatedAt:       definition.UpdatedAt,
		}
		if err := db.Create(&profile).Error; err != nil {
			return err
		}
	}
	return nil
}

func nextWorkerProfileID(db *gorm.DB, sourceID string) (string, error) {
	base := "worker_profile_" + sourceID
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
		var count int64
		if err := db.Model(&model.WorkerProfile{}).
			Where("id = ?", candidate).
			Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
}
