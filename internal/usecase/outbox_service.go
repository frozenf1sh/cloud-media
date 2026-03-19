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
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"github.com/google/uuid"
	"github.com/google/wire"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// OutboxServiceProviderSet OutboxService 的 Wire 提供者集合
var OutboxServiceProviderSet = wire.NewSet(
	NewOutboxService,
)

// OutboxConfig OutboxService 配置
type OutboxConfig struct {
	RecoveryInterval  time.Duration
	PendingTaskMaxAge time.Duration
	BatchSize         int
	MaxRetries        int
}

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

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewOutboxService 创建 OutboxService 实例
func NewOutboxService(
	outboxRepo domain.OutboxRepository,
	taskRepo domain.VideoTaskRepository,
	msgRepo domain.ProcessedMessageRepository,
	broker domain.ReliableMQBroker,
	db *persistence.Database,
	cfg OutboxConfig,
) *OutboxService {
	ctx, cancel := context.WithCancel(context.Background())

	return &OutboxService{
		outboxRepo:        outboxRepo,
		taskRepo:          taskRepo,
		msgRepo:           msgRepo,
		broker:            broker,
		db:                db.DB,
		recoveryInterval:  cfg.RecoveryInterval,
		pendingTaskMaxAge: cfg.PendingTaskMaxAge,
		batchSize:         cfg.BatchSize,
		maxRetries:        cfg.MaxRetries,
		ctx:               ctx,
		cancel:            cancel,
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
		TraceID:       telemetry.TraceIDFromContext(ctx),
		SpanID:        telemetry.SpanIDFromContext(ctx),
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

	// 立即尝试发布，保持链路连续
	if err := s.publishSingleEvent(ctx, event); err != nil {
		// 发布失败只记录日志，不返回错误（后台会重试）
		logger.WarnContext(ctx, "Failed to publish immediately, will retry later",
			logger.String("event_id", event.EventID),
			logger.Err(err))
	}

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
	// 从 OutboxEvent 恢复 trace 上下文 - 仅当 ctx 没有 trace 时才恢复
	if event.TraceID != "" && !trace.SpanFromContext(ctx).SpanContext().HasTraceID() {
		ctx = telemetry.WithTraceSpanContext(ctx, event.TraceID, event.SpanID)
	}
	// 启动一个新的 span
	ctx, span := telemetry.StartSpan(ctx, "OutboxService.publishSingleEvent",
		telemetry.String("event_id", event.EventID),
		telemetry.String("aggregate_id", event.AggregateID),
	)
	defer span.End()

	var published bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 使用 SELECT FOR UPDATE 锁定行，检查状态是否还是 pending
		var outboxModel persistence.OutboxEventModel
		if err := tx.Raw("SELECT * FROM outbox_events WHERE event_id = ? FOR UPDATE", event.EventID).Scan(&outboxModel).Error; err != nil {
			return err
		}

		// 2. 如果状态不是 pending，说明已经被处理过了，直接返回
		currentStatus := domain.OutboxEventStatus(outboxModel.Status)
		if currentStatus != domain.OutboxStatusPending {
			logger.InfoContext(ctx, "Event already processed, skipping",
				logger.String("event_id", event.EventID),
				logger.String("status", string(currentStatus)))
			published = false
			return nil
		}

		// 3. 先更新状态为 processing（乐观锁）
		now := time.Now()
		result := tx.Model(&persistence.OutboxEventModel{}).
			Where("event_id = ? AND status = ?", event.EventID, domain.OutboxStatusPending).
			Updates(map[string]interface{}{
				"status":       "processing",
				"processed_at": &now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 没有更新到行，说明其他实例已经处理了
			logger.InfoContext(ctx, "Event already processed by another instance, skipping",
				logger.String("event_id", event.EventID))
			published = false
			return nil
		}

		// 4. 发布到消息队列
		if err := s.broker.PublishWithConfirm(ctx, event); err != nil {
			// 发布失败，增加重试计数，恢复为 pending 状态
			tx.Model(&persistence.OutboxEventModel{}).
				Where("event_id = ?", event.EventID).
				Updates(map[string]interface{}{
					"status":       domain.OutboxStatusPending,
					"retry_count":  gorm.Expr("retry_count + 1"),
					"last_error":   err.Error(),
					"processed_at": nil,
				})

			// 检查是否超过最大重试次数
			if outboxModel.RetryCount+1 >= outboxModel.MaxRetries {
				tx.Model(&persistence.OutboxEventModel{}).
					Where("event_id = ?", event.EventID).
					Updates(map[string]interface{}{
						"status":       domain.OutboxStatusFailed,
						"last_error":   fmt.Sprintf("max retries exceeded: %v", err),
					})
				logger.ErrorContext(ctx, "Event failed permanently",
					logger.String("event_id", event.EventID),
					logger.Err(err))
				telemetry.RecordError(ctx, err)
			}

			return err
		}

		// 5. 发布成功，标记为已发布
		result = tx.Model(&persistence.OutboxEventModel{}).
			Where("event_id = ?", event.EventID).
			Updates(map[string]interface{}{
				"status": domain.OutboxStatusPublished,
			})
		if result.Error != nil {
			logger.WarnContext(ctx, "Failed to mark event as published",
				logger.String("event_id", event.EventID),
				logger.Err(result.Error))
			// 只是标记失败，不回滚（消息已经发了）
		}

		published = true
		return nil
	})

	if err != nil {
		return err
	}

	if published {
		logger.InfoContext(ctx, "Event published successfully",
			logger.String("event_id", event.EventID),
			logger.String("aggregate_id", event.AggregateID))
	}

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
		// 先检查是否已经有 outbox 事件，避免重复创建
		// 这里我们通过事务 + 状态更新来确保原子性
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 1. 检查任务是否已经是 queued 状态
			var taskModel persistence.VideoTaskModel
			if err := tx.Where("task_id = ?", task.TaskID).First(&taskModel).Error; err != nil {
				return err
			}

			// 如果已经不是 pending 状态，跳过
			if domain.VideoTaskStatus(taskModel.Status) != domain.TaskStatusPending {
				return nil
			}

			// 2. 检查是否已经有 outbox 事件
			var outboxCount int64
			if err := tx.Model(&persistence.OutboxEventModel{}).
				Where("aggregate_id = ? AND aggregate_type = ?", task.TaskID, "video_task").
				Count(&outboxCount).Error; err != nil {
				return err
			}

			// 如果已经有 outbox 事件，只更新任务状态
			if outboxCount > 0 {
				// 更新任务状态为 queued
				updates := map[string]interface{}{
					"status": string(domain.TaskStatusQueued),
				}
				return tx.Model(&taskModel).Updates(updates).Error
			}

			// 3. 序列化任务
			payload, err := json.Marshal(task)
			if err != nil {
				return err
			}

			// 4. 创建 outbox 事件
			event := &domain.OutboxEvent{
				EventID:       uuid.New().String(),
				EventType:     "video_task.created",
				AggregateID:   task.TaskID,
				AggregateType: "video_task",
				Payload:       payload,
				TraceID:       task.TraceID, // 使用任务保存的 TraceID
				Status:        domain.OutboxStatusPending,
				RetryCount:    0,
				MaxRetries:    s.maxRetries,
				CreatedAt:     time.Now(),
			}

			eventModel := persistence.OutboxEventFromDomain(event)
			if err := tx.Create(eventModel).Error; err != nil {
				return err
			}

			// 5. 更新任务状态为 queued
			updates := map[string]interface{}{
				"status": string(domain.TaskStatusQueued),
			}
			return tx.Model(&taskModel).Updates(updates).Error
		})

		if err != nil {
			logger.ErrorContext(ctx, "Failed to requeue pending task",
				logger.String("task_id", task.TaskID),
				logger.Err(err))
			continue
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
