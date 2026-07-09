package repository

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/events"
	"redpanda/protocol/permission"
)

type PermissionRequestRepository struct {
	db *gorm.DB
}

func NewPermissionRequestRepository(db *gorm.DB) PermissionRequestRepository {
	return PermissionRequestRepository{db: db}
}

func (r PermissionRequestRepository) ProjectRequired(event events.Envelope) error {
	if event.Type != events.EventPermissionRequest {
		return nil
	}
	permissionID := stringPayload(event.Payload, "permission_id")
	if permissionID == "" {
		return nil
	}
	argsRaw, _ := json.Marshal(anyPayload(event.Payload, "arguments"))
	now := event.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := model.PermissionRequest{
		ID:            permissionID,
		RunID:         firstNonEmpty(stringPayload(event.Payload, "run_id"), event.RootRunID),
		SessionID:     event.SessionID,
		ToolCallID:    stringPayload(event.Payload, "tool_call_id"),
		ToolName:      stringPayload(event.Payload, "tool_name"),
		Risk:          stringPayload(event.Payload, "risk"),
		Summary:       stringPayload(event.Payload, "summary"),
		Detail:        stringPayload(event.Payload, "detail"),
		ArgumentsJSON: string(argsRaw),
		Status:        "pending",
		RootSeq:       event.RootSeq,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"run_id":         row.RunID,
			"session_id":     row.SessionID,
			"tool_call_id":   row.ToolCallID,
			"tool_name":      row.ToolName,
			"risk":           row.Risk,
			"summary":        row.Summary,
			"detail":         row.Detail,
			"arguments_json": row.ArgumentsJSON,
			"status":         row.Status,
			"root_seq":       row.RootSeq,
			"created_at":     row.CreatedAt,
			"updated_at":     row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func (r PermissionRequestRepository) Resolve(params permission.ResolveParams) error {
	now := time.Now().UTC()
	status := "resolved"
	if params.Decision == permission.DecisionDeny {
		status = "denied"
	}
	return r.db.Model(&model.PermissionRequest{}).
		Where("id = ?", params.PermissionID).
		Updates(map[string]any{
			"status":      status,
			"decision":    string(params.Decision),
			"reason":      params.Reason,
			"resolved_at": &now,
			"updated_at":  now,
		}).Error
}

func (r PermissionRequestRepository) ClosePendingByRun(runID string, status string, reason string) error {
	now := time.Now().UTC()
	return r.db.Model(&model.PermissionRequest{}).
		Where("run_id = ? AND status = ?", runID, "pending").
		Updates(map[string]any{
			"status":      status,
			"reason":      reason,
			"resolved_at": &now,
			"updated_at":  now,
		}).Error
}

func (r PermissionRequestRepository) Get(permissionID string) (model.PermissionRequest, error) {
	var row model.PermissionRequest
	err := r.db.First(&row, "id = ?", permissionID).Error
	return row, err
}

func (r PermissionRequestRepository) ListPending(limit int) ([]model.PermissionRequest, error) {
	return r.list("status = ?", []any{"pending"}, limit)
}

func (r PermissionRequestRepository) ListByRun(runID string, limit int) ([]model.PermissionRequest, error) {
	return r.list("run_id = ?", []any{runID}, limit)
}

func (r PermissionRequestRepository) ListBySession(sessionID string, limit int) ([]model.PermissionRequest, error) {
	return r.list("session_id = ?", []any{sessionID}, limit)
}

func (r PermissionRequestRepository) list(where string, args []any, limit int) ([]model.PermissionRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.PermissionRequest
	query := r.db.Order("created_at desc").Limit(limit)
	if where != "" {
		query = query.Where(where, args...)
	}
	err := query.Find(&rows).Error
	return rows, err
}
