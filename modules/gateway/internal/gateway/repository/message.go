package repository

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/methods"
)

var messageIDCounter atomic.Uint64

type MessageRepository struct {
	db *gorm.DB
}

func NewMessageRepository(db *gorm.DB) MessageRepository {
	return MessageRepository{db: db}
}

func (r MessageRepository) Add(sessionID string, role string, text string, runID string) (model.Message, error) {
	seq, err := r.nextSeq(sessionID)
	if err != nil {
		return model.Message{}, err
	}
	content, err := json.Marshal([]methods.ContentBlock{{Type: "text", Text: text}})
	if err != nil {
		return model.Message{}, err
	}
	now := time.Now().UTC()
	message := model.Message{
		ID:          newMessageID(now),
		SessionID:   sessionID,
		Role:        role,
		ContentJSON: string(content),
		Seq:         seq,
		RunID:       runID,
		CreatedAt:   now,
	}
	return message, r.db.Create(&message).Error
}

func (r MessageRepository) AddWithMetadata(sessionID string, role string, content []methods.ContentBlock, runID string, metadataJSON string) (model.Message, error) {
	seq, err := r.nextSeq(sessionID)
	if err != nil {
		return model.Message{}, err
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return model.Message{}, err
	}
	now := time.Now().UTC()
	message := model.Message{
		ID:           newMessageID(now),
		SessionID:    sessionID,
		Role:         role,
		ContentJSON:  string(encoded),
		Seq:          seq,
		RunID:        runID,
		MetadataJSON: metadataJSON,
		CreatedAt:    now,
	}
	return message, r.db.Create(&message).Error
}

func (r MessageRepository) AddOrAppend(sessionID string, role string, text string, runID string) (model.Message, error) {
	if !isAppendableDeltaRole(role) {
		return r.Add(sessionID, role, text, runID)
	}

	var last model.Message
	err := r.db.Where("session_id = ?", sessionID).Order("seq desc").First(&last).Error
	if err == gorm.ErrRecordNotFound {
		return r.Add(sessionID, role, text, runID)
	}
	if err != nil {
		return model.Message{}, err
	}
	if last.Role != role || last.RunID != runID {
		return r.Add(sessionID, role, text, runID)
	}

	var content []methods.ContentBlock
	if last.ContentJSON != "" {
		if err := json.Unmarshal([]byte(last.ContentJSON), &content); err != nil {
			return model.Message{}, err
		}
	}
	if len(content) == 0 {
		content = []methods.ContentBlock{{Type: "text"}}
	}
	content[len(content)-1].Type = "text"
	content[len(content)-1].Text += text

	encoded, err := json.Marshal(content)
	if err != nil {
		return model.Message{}, err
	}
	last.ContentJSON = string(encoded)
	return last, r.db.Save(&last).Error
}

func isAppendableDeltaRole(role string) bool {
	return role == "assistant" || role == "subagent"
}

func (r MessageRepository) List(sessionID string, limit int) ([]model.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.Message
	err := r.db.Where("session_id = ?", sessionID).Order("seq asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r MessageRepository) ListLatest(sessionID string, limit int) ([]model.Message, error) {
	return r.listLatestQuery(r.db.Where("session_id = ?", sessionID), limit)
}

func (r MessageRepository) ListLatestConversation(sessionID string, limit int) ([]model.Message, error) {
	return r.listLatestQuery(r.db.Where("session_id = ? AND role IN ?", sessionID, []string{"user", "assistant"}), limit)
}

func (r MessageRepository) ListConversationAfterSeq(sessionID string, afterSeq uint64, limit int) ([]model.Message, error) {
	query := r.db.Where("session_id = ? AND role IN ? AND seq > ?", sessionID, []string{"user", "assistant"}, afterSeq)
	return r.listLatestQuery(query, limit)
}

func (r MessageRepository) listLatestQuery(query *gorm.DB, limit int) ([]model.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.Message
	if err := query.Order("seq desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, nil
}

func (r MessageRepository) ListThroughSeq(sessionID string, throughSeq uint64) ([]model.Message, error) {
	var rows []model.Message
	query := r.db.Where("session_id = ?", sessionID)
	if throughSeq > 0 {
		query = query.Where("seq <= ?", throughSeq)
	}
	err := query.Order("seq asc").Find(&rows).Error
	return rows, err
}

func (r MessageRepository) ListRange(sessionID string, startSeq uint64, endSeq uint64) ([]model.Message, error) {
	var rows []model.Message
	query := r.db.Where("session_id = ?", sessionID)
	if startSeq > 0 {
		query = query.Where("seq >= ?", startSeq)
	}
	if endSeq > 0 {
		query = query.Where("seq <= ?", endSeq)
	}
	err := query.Order("seq asc").Find(&rows).Error
	return rows, err
}

func (r MessageRepository) LatestSeq(sessionID string) (uint64, error) {
	var last model.Message
	err := r.db.Where("session_id = ?", sessionID).Order("seq desc").First(&last).Error
	if err == gorm.ErrRecordNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return last.Seq, nil
}

func (r MessageRepository) CopyToSession(source []model.Message, targetSessionID string, startSeq uint64) (int, error) {
	now := time.Now().UTC()
	for index, message := range source {
		copied := message
		copied.ID = newMessageID(now)
		copied.SessionID = targetSessionID
		copied.Seq = startSeq + uint64(index)
		copied.SourceMessageID = message.ID
		copied.CreatedAt = now.Add(time.Duration(index) * time.Nanosecond)
		if err := r.db.Create(&copied).Error; err != nil {
			return index, err
		}
	}
	return len(source), nil
}

func newMessageID(now time.Time) string {
	return fmt.Sprintf("msg_%d_%d", now.UnixNano(), messageIDCounter.Add(1))
}

func (r MessageRepository) nextSeq(sessionID string) (uint64, error) {
	var last model.Message
	err := r.db.Where("session_id = ?", sessionID).Order("seq desc").First(&last).Error
	if err == gorm.ErrRecordNotFound {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return last.Seq + 1, nil
}
