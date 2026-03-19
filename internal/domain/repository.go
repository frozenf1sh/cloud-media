package domain

import (
	"context"
	"time"
)

// VideoTaskRepository 任务仓储接口
type VideoTaskRepository interface {
	Create(ctx context.Context, task *VideoTask) error
	Update(ctx context.Context, task *VideoTask) error
	GetByTaskID(ctx context.Context, taskID string) (*VideoTask, error)
	List(ctx context.Context, page, pageSize int) ([]*VideoTask, int64, error)
	UpdateStatus(ctx context.Context, taskID string, status VideoTaskStatus, message ...string) error
	UpdateProgress(ctx context.Context, taskID string, progress int) error
	// TryTransitionToProcessing 原子性地尝试将任务从 pending/queued 转换为 processing
	// 返回值: 成功时返回更新后的任务，失败时返回错误
	TryTransitionToProcessing(ctx context.Context, taskID string) (*VideoTask, error)
	// ListPendingTasks 列出所有 pending/queued 状态的任务（用于恢复）
	ListPendingTasks(ctx context.Context, maxAge time.Duration) ([]*VideoTask, error)
}

// OutboxRepository Outbox 仓储接口
type OutboxRepository interface {
	// CreateOutboxEvent 创建 outbox 事件（在同一个事务中）
	CreateOutboxEvent(ctx context.Context, event *OutboxEvent) error
	// GetPendingEvents 获取待发布的事件
	GetPendingEvents(ctx context.Context, limit int) ([]*OutboxEvent, error)
	// MarkAsPublished 标记事件为已发布
	MarkAsPublished(ctx context.Context, eventID string) error
	// MarkAsFailed 标记事件为失败
	MarkAsFailed(ctx context.Context, eventID string, err string) error
	// IncrementRetry 增加重试计数
	IncrementRetry(ctx context.Context, eventID string, err string) error
}

// ProcessedMessageRepository 已处理消息仓储接口
type ProcessedMessageRepository interface {
	// TryMarkAsProcessed 尝试标记消息为已处理（原子操作，返回是否成功）
	// 如果消息已处理过，返回 false
	TryMarkAsProcessed(ctx context.Context, messageID, consumerID string) (bool, error)
	// CleanupOldEntries 清理旧记录
	CleanupOldEntries(ctx context.Context, olderThan time.Duration) error
}
