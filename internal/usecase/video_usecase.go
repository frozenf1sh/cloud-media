package usecase

import (
	"context"
	"fmt"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/google/wire"
)

// ProviderSet 是 Wire 的提供者集合
var ProviderSet = wire.NewSet(NewVideoUseCase, WorkerProviderSet)

// VideoUseCase 视频处理用例
type VideoUseCase struct {
	mq         domain.MQBroker
	repository domain.VideoTaskRepository
}

// NewVideoUseCase 创建 VideoUseCase 实例
func NewVideoUseCase(mq domain.MQBroker, repo domain.VideoTaskRepository) *VideoUseCase {
	return &VideoUseCase{
		mq:         mq,
		repository: repo,
	}
}

// SubmitTranscodeTask 提交转码任务
func (uc *VideoUseCase) SubmitTranscodeTask(ctx context.Context, taskID, sourceBucket, sourceKey string) (*domain.VideoTask, error) {
	// 从上下文中提取 Trace ID
	traceID := logger.FromContext(ctx)

	// 1. 创建任务领域对象
	task := &domain.VideoTask{
		TaskID:       taskID,
		TraceID:      traceID,
		SourceKey:    sourceKey,
		SourceBucket: sourceBucket,
		Status:       domain.TaskStatusPending,
	}

	// 2. 保存到数据库
	if err := uc.repository.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to save task: %w", err)
	}

	// 3. 更新状态为 queued
	if err := uc.repository.UpdateStatus(ctx, taskID, domain.TaskStatusQueued); err != nil {
		return nil, fmt.Errorf("failed to update task status: %w", err)
	}
	task.Status = domain.TaskStatusQueued

	// 4. 发布到消息队列
	if err := uc.mq.PublishVideoTask(task); err != nil {
		// 即使 MQ 发布失败，任务已经在数据库中，可以通过重试机制处理
		// 这里不返回错误，而是记录日志
		_ = uc.repository.UpdateStatus(ctx, taskID, domain.TaskStatusPending, "queue publish failed, pending retry")
		return task, fmt.Errorf("failed to publish to queue: %w", err)
	}

	return task, nil
}

// GetTaskStatus 获取任务状态
func (uc *VideoUseCase) GetTaskStatus(ctx context.Context, taskID string) (*domain.VideoTask, error) {
	return uc.repository.GetByTaskID(ctx, taskID)
}

// ListTasks 列出任务
func (uc *VideoUseCase) ListTasks(ctx context.Context, page, pageSize int) ([]*domain.VideoTask, int64, error) {
	return uc.repository.List(ctx, page, pageSize)
}

// CancelTask 取消任务
func (uc *VideoUseCase) CancelTask(ctx context.Context, taskID string) error {
	return uc.repository.UpdateStatus(ctx, taskID, domain.TaskStatusCancelled, "cancelled by user")
}
