package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"gorm.io/gorm"
)

// OutboxEventModel GORM 模型 - 对应 outbox_events 表（Transactional Outbox 模式）
type OutboxEventModel struct {
	ID            uint      `gorm:"primaryKey;autoIncrement"`
	EventID       string    `gorm:"size:64;uniqueIndex;not null"`
	EventType     string    `gorm:"size:64;not null;index"`
	AggregateID   string    `gorm:"size:64;index"`
	AggregateType string    `gorm:"size:64;index"`
	Payload       []byte    `gorm:"type:bytea;not null"`
	TraceID       string    `gorm:"size:64;index"`
	SpanID        string    `gorm:"size:32;index"`
	Status        string    `gorm:"size:32;index;not null"`
	RetryCount    int       `gorm:"default:0"`
	MaxRetries    int       `gorm:"default:10"`
	LastError     string    `gorm:"type:text"`
	CreatedAt     time.Time `gorm:"index"`
	ProcessedAt   *time.Time
}

// TableName 指定表名
func (OutboxEventModel) TableName() string {
	return "outbox_events"
}

// ToDomain 转换为领域模型
func (m *OutboxEventModel) ToDomain() *domain.OutboxEvent {
	return &domain.OutboxEvent{
		ID:            m.ID,
		EventID:       m.EventID,
		EventType:     m.EventType,
		AggregateID:   m.AggregateID,
		AggregateType: m.AggregateType,
		Payload:       append([]byte(nil), m.Payload...),
		TraceID:       m.TraceID,
		SpanID:        m.SpanID,
		Status:        domain.OutboxEventStatus(m.Status),
		RetryCount:    m.RetryCount,
		MaxRetries:    m.MaxRetries,
		LastError:     m.LastError,
		CreatedAt:     m.CreatedAt,
		ProcessedAt:   m.ProcessedAt,
	}
}

// OutboxEventFromDomain 从领域模型创建 GORM 模型
func OutboxEventFromDomain(event *domain.OutboxEvent) *OutboxEventModel {
	return &OutboxEventModel{
		ID:            event.ID,
		EventID:       event.EventID,
		EventType:     event.EventType,
		AggregateID:   event.AggregateID,
		AggregateType: event.AggregateType,
		Payload:       append([]byte(nil), event.Payload...),
		TraceID:       event.TraceID,
		SpanID:        event.SpanID,
		Status:        string(event.Status),
		RetryCount:    event.RetryCount,
		MaxRetries:    event.MaxRetries,
		LastError:     event.LastError,
		CreatedAt:     event.CreatedAt,
		ProcessedAt:   event.ProcessedAt,
	}
}

// outboxRepository OutboxRepository 的 GORM 实现
type outboxRepository struct {
	db *gorm.DB
}

// NewOutboxRepository 创建 OutboxRepository 实例
func NewOutboxRepository(db *gorm.DB) domain.OutboxRepository {
	return &outboxRepository{db: db}
}

// CreateOutboxEvent 创建 outbox 事件
func (r *outboxRepository) CreateOutboxEvent(ctx context.Context, event *domain.OutboxEvent) error {
	model := OutboxEventFromDomain(event)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to create outbox event: %w", err)
	}
	event.ID = model.ID
	return nil
}

// GetPendingEvents 获取待发布的事件
func (r *outboxRepository) GetPendingEvents(ctx context.Context, limit int) ([]*domain.OutboxEvent, error) {
	var models []OutboxEventModel

	err := r.db.WithContext(ctx).
		Where("status = ? AND retry_count < max_retries", string(domain.OutboxStatusPending)).
		Order("created_at ASC").
		Limit(limit).
		Find(&models).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get pending outbox events: %w", err)
	}

	events := make([]*domain.OutboxEvent, len(models))
	for i, m := range models {
		events[i] = m.ToDomain()
	}
	return events, nil
}

// MarkAsPublished 标记事件为已发布
func (r *outboxRepository) MarkAsPublished(ctx context.Context, eventID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&OutboxEventModel{}).
		Where("event_id = ?", eventID).
		Updates(map[string]interface{}{
			"status":       string(domain.OutboxStatusPublished),
			"processed_at": &now,
		}).Error
}

// MarkAsFailed 标记事件为失败
func (r *outboxRepository) MarkAsFailed(ctx context.Context, eventID string, err string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&OutboxEventModel{}).
		Where("event_id = ?", eventID).
		Updates(map[string]interface{}{
			"status":       string(domain.OutboxStatusFailed),
			"last_error":   err,
			"processed_at": &now,
		}).Error
}

// IncrementRetry 增加重试计数
func (r *outboxRepository) IncrementRetry(ctx context.Context, eventID string, err string) error {
	return r.db.WithContext(ctx).
		Model(&OutboxEventModel{}).
		Where("event_id = ?", eventID).
		Updates(map[string]interface{}{
			"retry_count": gorm.Expr("retry_count + 1"),
			"last_error":  err,
		}).Error
}

// ProcessedMessageModel GORM 模型 - 对应 processed_messages 表（用于去重）
type ProcessedMessageModel struct {
	ID          uint      `gorm:"primaryKey;autoIncrement"`
	MessageID   string    `gorm:"size:128;not null"`
	ConsumerID  string    `gorm:"size:128;not null"`
	ProcessedAt time.Time `gorm:"not null"`
}

// TableName 指定表名
func (ProcessedMessageModel) TableName() string {
	return "processed_messages"
}

// UniqueIndex 复合唯一索引：message_id + consumer_id
func (ProcessedMessageModel) UniqueIndex() [2]string {
	return [2]string{"message_id", "consumer_id"}
}

// processedMessageRepository ProcessedMessageRepository 的 GORM 实现
type processedMessageRepository struct {
	db *gorm.DB
}

// NewProcessedMessageRepository 创建 ProcessedMessageRepository 实例
func NewProcessedMessageRepository(db *gorm.DB) domain.ProcessedMessageRepository {
	return &processedMessageRepository{db: db}
}

// TryMarkAsProcessed 尝试标记消息为已处理（原子操作）
func (r *processedMessageRepository) TryMarkAsProcessed(ctx context.Context, messageID, consumerID string) (bool, error) {
	var inserted bool

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先检查是否已存在
		var count int64
		if err := tx.Model(&ProcessedMessageModel{}).
			Where("message_id = ? AND consumer_id = ?", messageID, consumerID).
			Count(&count).Error; err != nil {
			return err
		}

		if count > 0 {
			inserted = false
			return nil
		}

		// 插入新记录
		now := time.Now()
		if err := tx.Create(&ProcessedMessageModel{
			MessageID:   messageID,
			ConsumerID:  consumerID,
			ProcessedAt: now,
		}).Error; err != nil {
			// 检查是否是唯一约束冲突
			if tx.Error != nil && (tx.Error.Error() == "UNIQUE constraint failed" ||
				tx.Error.Error() == "duplicate key value violates unique constraint") {
				inserted = false
				return nil
			}
			return err
		}

		inserted = true
		return nil
	})

	if err != nil {
		return false, fmt.Errorf("failed to mark message as processed: %w", err)
	}

	return inserted, nil
}

// CleanupOldEntries 清理旧记录
func (r *processedMessageRepository) CleanupOldEntries(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return r.db.WithContext(ctx).
		Where("processed_at < ?", cutoff).
		Delete(&ProcessedMessageModel{}).Error
}
