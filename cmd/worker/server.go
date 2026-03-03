package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/broker"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
)

// Worker worker 服务
type Worker struct {
	broker   *broker.RabbitMQBroker
	useCase  *usecase.WorkerUseCase
	database *persistence.Database
}

// NewWorker 创建 Worker
func NewWorker(
	b *broker.RabbitMQBroker,
	uc *usecase.WorkerUseCase,
	db *persistence.Database,
) *Worker {
	return &Worker{
		broker:   b,
		useCase:  uc,
		database: db,
	}
}

// Run 运行 worker
func (w *Worker) Run(ctx context.Context) error {
	log := slog.With("trace_id", logger.FromContext(ctx))

	// 执行数据库迁移
	log.Info("Running database migration...")
	if err := w.database.AutoMigrate(); err != nil {
		log.Error("Failed to run migration", "error", err)
		return err
	}
	log.Info("Migration completed successfully")

	// 定义任务处理器
	handler := func(ctx context.Context, task *domain.VideoTask) error {
		return w.useCase.ProcessTask(ctx, task)
	}

	// 消费循环，支持重连
	for {
		select {
		case <-ctx.Done():
			log.Info("Context cancelled, stopping worker")
			w.broker.Close()
			return ctx.Err()
		default:
			// 尝试消费
			if err := w.broker.ConsumeTasks(ctx, handler); err != nil {
				if err == context.Canceled {
					return err
				}
				log.Error("Consume failed, attempting reconnect...", "error", err)

				// 等待后重连
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
				}

				if err := w.broker.Reconnect(); err != nil {
					log.Error("Reconnect failed", "error", err)
					continue
				}
				log.Info("Reconnected successfully")
			}
		}
	}
}
