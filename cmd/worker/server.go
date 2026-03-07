package main

import (
	"context"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/broker"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
	"github.com/frozenf1sh/cloud-media/pkg/health"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
)

// WorkerStatus worker 状态响应
type WorkerStatus struct {
	ActiveTasks int `json:"active_tasks"`
}

// Worker worker 服务
type Worker struct {
	broker   *broker.RabbitMQBroker
	useCase  *usecase.WorkerUseCase
	database *persistence.Database
	health   *health.Health
}

// NewWorker 创建 Worker
func NewWorker(
	b *broker.RabbitMQBroker,
	uc *usecase.WorkerUseCase,
	db *persistence.Database,
	storage domain.ObjectStorage,
) *Worker {
	// 确保必要的 bucket 存在
	ctx := context.Background()
	buckets := []string{"media-input", "media-output"}
	for _, bucket := range buckets {
		logger.Info("Ensuring bucket exists", logger.String("bucket", bucket))
		if err := storage.EnsureBucketExists(ctx, bucket); err != nil {
			logger.Warn("Failed to ensure bucket exists",
				logger.String("bucket", bucket),
				logger.Err(err))
		}
	}

	// 创建健康检查管理器
	healthChecker := health.New("worker", "1.0.0")

	// 添加数据库健康检查
	healthChecker.RegisterFunc("database", health.SimpleCheck(func(ctx context.Context) error {
		sqlDB, err := db.DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	}))

	return &Worker{
		broker:   b,
		useCase:  uc,
		database: db,
		health:   healthChecker,
	}
}

// Run 运行 worker
func (w *Worker) Run(ctx context.Context) error {
	// 执行数据库迁移
	logger.Info("Running database migration...")
	if err := w.database.AutoMigrate(); err != nil {
		logger.Error("Failed to run migration", logger.Err(err))
		return err
	}
	logger.Info("Migration completed successfully")

	// 定义任务处理器
	handler := func(ctx context.Context, task *domain.VideoTask) error {
		return w.useCase.ProcessTask(ctx, task)
	}

	// 消费循环，支持重连
	for {
		select {
		case <-ctx.Done():
			logger.Info("Context cancelled, stopping worker")
			w.broker.Close()
			return ctx.Err()
		default:
			// 尝试消费
			if err := w.broker.ConsumeTasks(ctx, handler); err != nil {
				if err == context.Canceled {
					return err
				}
				logger.Error("Consume failed, attempting reconnect...", logger.Err(err))

				// 等待后重连
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
				}

				if err := w.broker.Reconnect(); err != nil {
					logger.Error("Reconnect failed", logger.Err(err))
					continue
				}
				logger.Info("Reconnected successfully")
			}
		}
	}
}

// HealthChecker 返回健康检查器
func (w *Worker) HealthChecker() *health.Health {
	return w.health
}

// ActiveTaskCount 返回当前正在处理的任务数量
func (w *Worker) ActiveTaskCount() int {
	return w.useCase.ActiveTaskCount()
}
