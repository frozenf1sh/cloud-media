package domain

import "time"

// OutboxEventStatus Outbox 事件状态
type OutboxEventStatus string

const (
	OutboxStatusPending   OutboxEventStatus = "pending"
	OutboxStatusPublished OutboxEventStatus = "published"
	OutboxStatusFailed    OutboxEventStatus = "failed"
)

// OutboxEvent Outbox 事件（用于 Transactional Outbox 模式）
type OutboxEvent struct {
	ID            uint
	EventID       string           // 唯一事件 ID（用于幂等）
	EventType     string           // 事件类型
	AggregateID   string           // 聚合 ID（如 task_id）
	AggregateType string           // 聚合类型（如 "video_task"）
	Payload       []byte           // 事件内容（JSON）
	TraceID       string           // OpenTelemetry Trace ID
	SpanID        string           // OpenTelemetry Span ID
	Status        OutboxEventStatus
	RetryCount    int              // 重试次数
	MaxRetries    int              // 最大重试次数
	LastError     string           // 最后一次错误信息
	CreatedAt     time.Time
	ProcessedAt   *time.Time
}

// ProcessedMessage 已处理消息记录（用于去重/幂等消费）
type ProcessedMessage struct {
	ID          uint
	MessageID   string    // 消息唯一 ID
	ConsumerID  string    // 消费者 ID（区分不同服务/消费者）
	ProcessedAt time.Time // 处理时间
}
