
package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/google/uuid"
	"github.com/google/wire"
	"gorm.io/gorm"
)

// OutboxServiceProviderSet OutboxService 的 Wire 提供者集合
var OutboxServiceProviderSet = wire.NewSet(
	NewOutboxService,
)

const (
	// 默认最大重试次数
	defaultMaxRetries = 10
	// 默认恢复扫描间隔
	defaultRecoveryInterval = 30 * time.Second
	// 默认待处理任务最大存活时间
	defaultPendingTaskMaxAge = 1 * time.Hour
	// 批量处理大小
	defaultBatchSize = 10
)

// OutboxService 事务性发件箱服务
type OutboxService struct {
	outboxRepo        domain.OutboxRepository
	taskRepo          domain.VideoTaskRepository
	msgRepo           domain.ProcessedMessageRepository
	broker            domain.ReliableMQBroker
	db                *gorm.DB
	recoveryInterval  time.Duration
	pendingTaskMaxAge time.Duration
	batchSize         int
	maxRetries        int

	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	running           bool
}

// NewOutboxService 创建 OutboxService 实例
func NewOutboxService(
	outboxRepo domain.OutboxRepository,
	taskRepo domain.VideoTaskRepository,
	msgRepo domain.ProcessedMessageRepository,
	broker domain.ReliableMQBroker,
	db *persistence.Database,
) *OutboxService {
	ctx, cancel := context.WithCancel(context.Background())

	return &OutboxService{
		outboxRepo:        outboxRepo,
		taskRepo:          taskRepo,
		msgRepo:           msgRepo,
		broker:            broker,
		db:                db.DB,
		recoveryInterval:  defaultRecoveryInterval,
		pendingTaskMaxAge: defaultPendingTaskMaxAge,
		batchSize:         defaultBatchSize,
		maxRetries:        defaultMaxRetries,
		ctx:               ctx,
		cancel:            cancel,
	}
}

// SetRecoveryInterval 设置恢复扫描间隔
func (s *OutboxService) SetRecoveryInterval(interval time.Duration) {
	s.recoveryInterval = interval
}

// SetPendingTaskMaxAge 设置待处理任务最大存活时间
func (s *OutboxService) SetPendingTaskMaxAge(age time.Duration) {
	s.pendingTaskMaxAge = age
}

// SetBatchSize 设置批量处理大小
func (s *OutboxService) SetBatchSize(size int) {
	if size > 0 {
		s.batchSize = size
	}
}

// Start 启动 Outbox 服务
func (s *OutboxService) Start(ctx context.Context) error {
	// 启动 broker
	if err := s.broker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start broker: %w", err)
	}

	s.running = true

	// 启动恢复循环
	s.wg.Add(1)
	go s.recoveryLoop()

	logger.Info("Outbox service started")
	return nil
}

// Stop 停止 Outbox 服务
func (s *OutboxService) Stop() error {
	s.cancel()
	s.wg.Wait()

	if err := s.broker.Stop(); err != nil {
		logger.Warn("Error stopping broker", logger.Err(err))
	}

	s.running = false
	logger.Info("Outbox service stopped")
	return nil
}

// PublishVideoTaskTransactional 事务性发布视频任务
// 在同一个数据库事务中保存 video_task 和 outbox_event
func (s *OutboxService) PublishVideoTaskTransactional(
	ctx context.Context,
	task *domain.VideoTask,
) error {
	// 序列化任务
	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// 创建 outbox 事件
	event := &domain.OutboxEvent{
		EventID:       uuid.New().String(),
		EventType:     "video_task.created",
		AggregateID:   task.TaskID,
		AggregateType: "video_task",
		Payload:       payload,
		Status:        domain.OutboxStatusPending,
		RetryCount:    0,
		MaxRetries:    s.maxRetries,
		CreatedAt:     time.Now(),
	}

	// 在同一个事务中保存 video_task 和 outbox_event
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 保存 video_task
		taskModel := persistence.FromDomain(task)
		if err := tx.Create(taskModel).Error; err != nil {
			return fmt.Errorf("failed to create video task: %w", err)
		}
		// 回写 ID
		task.ID = taskModel.ID

		// 2. 保存 outbox_event
		eventModel := persistence.OutboxEventFromDomain(event)
		if err := tx.Create(eventModel).Error; err != nil {
			return fmt.Errorf("failed to create outbox event: %w", err)
		}
		// 回写 ID
		event.ID = eventModel.ID

		return nil
	})

	if err != nil {
		return err
	}

	logger.InfoContext(ctx, "Video task queued in outbox",
		logger.String("task_id", task.TaskID),
		logger.String("event_id", event.EventID))

	return nil
}

// PublishOutboxEvents 立即发布待处理的 outbox 事件
func (s *OutboxService) PublishOutboxEvents(ctx context.Context) error {
	events, err := s.outboxRepo.GetPendingEvents(ctx, s.batchSize)
	if err != nil {
		return fmt.Errorf("failed to get pending events: %w", err)
	}

	if len(events) == 0 {
		return nil
	}

	logger.InfoContext(ctx, "Publishing outbox events", logger.Int("count", len(events)))

	for _, event := range events {
		if err := s.publishSingleEvent(ctx, event); err != nil {
			logger.ErrorContext(ctx, "Failed to publish event",
				logger.String("event_id", event.EventID),
				logger.Err(err))
		}
	}

	return nil
}

// publishSingleEvent 发布单个事件
func (s *OutboxService) publishSingleEvent(ctx context.Context, event *domain.OutboxEvent) error {
	// 发布到消息队列
	if err := s.broker.PublishWithConfirm(ctx, event); err != nil {
		// 发布失败，增加重试计数
		_ = s.outboxRepo.IncrementRetry(ctx, event.EventID, err.Error())

		// 检查是否超过最大重试次数
		if event.RetryCount+1 >= event.MaxRetries {
			_ = s.outboxRepo.MarkAsFailed(ctx, event.EventID, fmt.Sprintf("max retries exceeded: %v", err))
			logger.ErrorContext(ctx, "Event failed permanently",
				logger.String("event_id", event.EventID),
				logger.Err(err))
		}

		return err
	}

	// 发布成功，标记为已发布
	if err := s.outboxRepo.MarkAsPublished(ctx, event.EventID); err != nil {
		logger.WarnContext(ctx, "Failed to mark event as published",
			logger.String("event_id", event.EventID),
			logger.Err(err))
	}

	logger.InfoContext(ctx, "Event published successfully",
		logger.String("event_id", event.EventID),
		logger.String("aggregate_id", event.AggregateID))

	return nil
}

// recoveryLoop 恢复循环：定时扫描和重试
func (s *OutboxService) recoveryLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.recoveryInterval)
	defer ticker.Stop()

	logger.Info("Recovery loop started",
		logger.Duration("interval", s.recoveryInterval))

	for {
		select {
		case <-s.ctx.Done():
			logger.Info("Recovery loop stopping")
			return

		case <-ticker.C:
			func() {
				ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
				defer cancel()

				// 1. 发布待处理的 outbox 事件
				if err := s.PublishOutboxEvents(ctx); err != nil {
					logger.ErrorContext(ctx, "Failed to publish outbox events", logger.Err(err))
				}

				// 2. 检查并恢复 pending/queued 状态的任务
				if err := s.recoverPendingTasks(ctx); err != nil {
					logger.ErrorContext(ctx, "Failed to recover pending tasks", logger.Err(err))
				}

				// 3. 清理旧的已处理消息记录
				if err := s.cleanupOldMessages(ctx); err != nil {
					logger.WarnContext(ctx, "Failed to cleanup old messages", logger.Err(err))
				}
			}()
		}
	}
}

// recoverPendingTasks 恢复待处理任务
func (s *OutboxService) recoverPendingTasks(ctx context.Context) error {
	tasks, err := s.taskRepo.ListPendingTasks(ctx, s.pendingTaskMaxAge)
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		return nil
	}

	logger.InfoContext(ctx, "Found pending tasks to recover", logger.Int("count", len(tasks)))

	for _, task := range tasks {
		// 检查是否已经有 outbox 事件
		// 这里简化处理：直接重新创建 outbox 事件
		if err := s.PublishVideoTaskTransactional(ctx, task); err != nil {
			logger.ErrorContext(ctx, "Failed to requeue pending task",
				logger.String("task_id", task.TaskID),
				logger.Err(err))
			continue
		}

		// 更新任务状态为 queued
		if err := s.taskRepo.UpdateStatus(ctx, task.TaskID, domain.TaskStatusQueued, "recovered by outbox service"); err != nil {
			logger.WarnContext(ctx, "Failed to update task status",
				logger.String("task_id", task.TaskID),
				logger.Err(err))
		}
	}

	return nil
}

// cleanupOldMessages 清理旧的已处理消息记录
func (s *OutboxService) cleanupOldMessages(ctx context.Context) error {
	// 保留 7 天
	return s.msgRepo.CleanupOldEntries(ctx, 7*24*time.Hour)
}

// TryMarkMessageProcessed 尝试标记消息为已处理（幂等消费）
func (s *OutboxService) TryMarkMessageProcessed(
	ctx context.Context,
	messageID string,
	consumerID string,
) (bool, error) {
	return s.msgRepo.TryMarkAsProcessed(ctx, messageID, consumerID)
}
