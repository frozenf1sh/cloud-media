package usecase

import (
	"context"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/errors"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"github.com/google/uuid"
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
	ctx, span := telemetry.StartSpan(ctx, "VideoUseCase.SubmitTranscodeTask",
		telemetry.String("task_id", taskID),
		telemetry.String("source_bucket", sourceBucket),
		telemetry.String("source_key", sourceKey),
	)
	defer span.End()

	// 1. 参数验证
	if sourceBucket == "" {
		err := errors.InvalidArgument("source_bucket is required")
		telemetry.RecordError(ctx, err)
		return nil, err
	}
	if sourceKey == "" {
		err := errors.InvalidArgument("source_key is required")
		telemetry.RecordError(ctx, err)
		return nil, err
	}

	// 如果客户端没有提供 taskID，由服务端生成
	if taskID == "" {
		taskID = uuid.New().String()
	}

	// 从上下文中提取 Trace ID
	traceID := telemetry.TraceIDFromContext(ctx)

	// 2. 检查任务是否已存在
	existingTask, _ := uc.repository.GetByTaskID(ctx, taskID)
	if existingTask != nil {
		err := errors.AlreadyExistsf("task %s already exists", taskID)
		telemetry.RecordError(ctx, err)
		return nil, err
	}

	// 3. 创建任务领域对象
	task := &domain.VideoTask{
		TaskID:       taskID,
		TraceID:      traceID,
		SourceKey:    sourceKey,
		SourceBucket: sourceBucket,
		Status:       domain.TaskStatusPending,
	}

	// 4. 保存到数据库
	if err := uc.repository.Create(ctx, task); err != nil {
		telemetry.RecordError(ctx, err)
		return nil, errors.InternalWrap("failed to save task", err)
	}

	// 5. 更新状态为 queued
	if err := uc.repository.UpdateStatus(ctx, taskID, domain.TaskStatusQueued); err != nil {
		telemetry.RecordError(ctx, err)
		return nil, errors.InternalWrap("failed to update task status", err)
	}
	task.Status = domain.TaskStatusQueued

	// 6. 发布到消息队列
	if err := uc.mq.PublishVideoTask(ctx, task); err != nil {
		// 即使 MQ 发布失败，任务已经在数据库中，可以通过重试机制处理
		telemetry.RecordError(ctx, err)
		_ = uc.repository.UpdateStatus(ctx, taskID, domain.TaskStatusPending, "queue publish failed, pending retry")
		// 不返回错误给客户端，因为任务已经持久化
	}

	logger.InfoContext(ctx, "Task submitted successfully",
		logger.String("task_id", taskID),
		logger.String("source_bucket", sourceBucket),
		logger.String("source_key", sourceKey),
	)

	// 设置 span 状态为成功
	telemetry.SetSpanStatusOK(ctx)

	return task, nil
}

// GetTaskStatus 获取任务状态
func (uc *VideoUseCase) GetTaskStatus(ctx context.Context, taskID string) (*domain.VideoTask, error) {
	ctx, span := telemetry.StartSpan(ctx, "VideoUseCase.GetTaskStatus",
		telemetry.String("task_id", taskID),
	)
	defer span.End()

	// 参数验证
	if taskID == "" {
		err := errors.InvalidArgument("task_id is required")
		telemetry.RecordError(ctx, err)
		return nil, err
	}

	task, err := uc.repository.GetByTaskID(ctx, taskID)
	if err != nil {
		telemetry.RecordError(ctx, err)
		return nil, err
	}

	telemetry.SetSpanStatusOK(ctx)
	return task, nil
}

// ListTasks 列出任务
func (uc *VideoUseCase) ListTasks(ctx context.Context, page, pageSize int) ([]*domain.VideoTask, int64, error) {
	ctx, span := telemetry.StartSpan(ctx, "VideoUseCase.ListTasks",
		telemetry.Int("page", page),
		telemetry.Int("page_size", pageSize),
	)
	defer span.End()

	// 参数验证
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := uc.repository.List(ctx, page, pageSize)
	if err != nil {
		telemetry.RecordError(ctx, err)
		return nil, 0, err
	}

	telemetry.SetSpanStatusOK(ctx)
	return tasks, total, nil
}

// CancelTask 取消任务
func (uc *VideoUseCase) CancelTask(ctx context.Context, taskID string) error {
	ctx, span := telemetry.StartSpan(ctx, "VideoUseCase.CancelTask",
		telemetry.String("task_id", taskID),
	)
	defer span.End()

	// 参数验证
	if taskID == "" {
		err := errors.InvalidArgument("task_id is required")
		telemetry.RecordError(ctx, err)
		return err
	}

	logger.InfoContext(ctx, "Cancelling task", logger.String("task_id", taskID))
	err := uc.repository.UpdateStatus(ctx, taskID, domain.TaskStatusCancelled, "cancelled by user")
	if err != nil {
		telemetry.RecordError(ctx, err)
		return err
	}

	telemetry.SetSpanStatusOK(ctx)
	return nil
}
