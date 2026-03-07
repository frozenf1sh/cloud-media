package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/google/wire"
	"gorm.io/gorm"
)

// RepositoryProviderSet 是 Repository 的 Wire 提供者集合
var RepositoryProviderSet = wire.NewSet(
	NewVideoTaskRepository,
)

// videoTaskRepository VideoTaskRepository 的 GORM 实现
type videoTaskRepository struct {
	db *gorm.DB
}

// NewVideoTaskRepository 创建 VideoTaskRepository 实例
func NewVideoTaskRepository(db *gorm.DB) domain.VideoTaskRepository {
	return &videoTaskRepository{db: db}
}

// Create 创建新任务
func (r *videoTaskRepository) Create(ctx context.Context, task *domain.VideoTask) error {
	model := FromDomain(task)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to create video task: %w", err)
	}
	// 回写 ID
	task.ID = model.ID
	return nil
}

// Update 更新任务
func (r *videoTaskRepository) Update(ctx context.Context, task *domain.VideoTask) error {
	model := FromDomain(task)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update video task: %w", err)
	}
	return nil
}

// GetByTaskID 根据 TaskID 获取任务
func (r *videoTaskRepository) GetByTaskID(ctx context.Context, taskID string) (*domain.VideoTask, error) {
	var model VideoTaskModel
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		First(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("task not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return model.ToDomain(), nil
}

// List 分页获取任务列表
func (r *videoTaskRepository) List(ctx context.Context, page, pageSize int) ([]*domain.VideoTask, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var models []VideoTaskModel
	var total int64

	// 获取总数
	if err := r.db.WithContext(ctx).Model(&VideoTaskModel{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count tasks: %w", err)
	}

	// 分页查询
	offset := (page - 1) * pageSize
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&models).Error
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list tasks: %w", err)
	}

	// 转换为领域模型
	tasks := make([]*domain.VideoTask, len(models))
	for i, m := range models {
		tasks[i] = m.ToDomain()
	}

	return tasks, total, nil
}

// UpdateStatus 更新任务状态（原子操作 + 记录日志）
func (r *videoTaskRepository) UpdateStatus(ctx context.Context, taskID string, status domain.VideoTaskStatus, message ...string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 获取当前任务
		var model VideoTaskModel
		if err := tx.Where("task_id = ?", taskID).First(&model).Error; err != nil {
			return fmt.Errorf("task not found: %w", err)
		}

		oldStatus := model.Status

		// 2. 更新状态
		updates := map[string]interface{}{
			"status": string(status),
		}

		// 如果状态变为 processing，记录开始时间
		if status == domain.TaskStatusProcessing && model.StartedAt == nil {
			now := tx.NowFunc()
			updates["started_at"] = &now
		}

		// 如果状态变为终态，记录完成时间
		if (status == domain.TaskStatusSuccess || status == domain.TaskStatusFailed || status == domain.TaskStatusCancelled) && model.CompletedAt == nil {
			now := tx.NowFunc()
			updates["completed_at"] = &now
		}

		if err := tx.Model(&model).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}

		// 3. 记录状态变更日志
		logMessage := ""
		if len(message) > 0 {
			logMessage = message[0]
		}
		logEntry := TaskStatusLogModel{
			TaskID:     taskID,
			FromStatus: oldStatus,
			ToStatus:   string(status),
			Message:    logMessage,
		}
		if err := tx.Create(&logEntry).Error; err != nil {
			return fmt.Errorf("failed to create status log: %w", err)
		}

		return nil
	})
}

// UpdateProgress 更新任务进度
func (r *videoTaskRepository) UpdateProgress(ctx context.Context, taskID string, progress int) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	err := r.db.WithContext(ctx).
		Model(&VideoTaskModel{}).
		Where("task_id = ?", taskID).
		Update("progress", progress).Error
	if err != nil {
		return fmt.Errorf("failed to update progress: %w", err)
	}
	return nil
}

// TryTransitionToProcessing 原子性地尝试将任务从 pending/queued 转换为 processing
func (r *videoTaskRepository) TryTransitionToProcessing(ctx context.Context, taskID string) (*domain.VideoTask, error) {
	var result *domain.VideoTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 使用 SELECT ... FOR UPDATE 锁定行，防止并发修改
		var model VideoTaskModel
		if err := tx.Raw("SELECT * FROM video_tasks WHERE task_id = ? FOR UPDATE", taskID).Scan(&model).Error; err != nil {
			return fmt.Errorf("task not found: %w", err)
		}

		// 2. 检查当前状态，只有 pending/queued 才能转换为 processing
		currentStatus := domain.VideoTaskStatus(model.Status)
		if currentStatus == domain.TaskStatusSuccess ||
			currentStatus == domain.TaskStatusFailed ||
			currentStatus == domain.TaskStatusCancelled {
			return fmt.Errorf("task already in terminal state: %s", currentStatus)
		}
		if currentStatus == domain.TaskStatusProcessing {
			return fmt.Errorf("task already being processed")
		}

		// 3. 更新状态为 processing 并设置 started_at
		now := tx.NowFunc()
		updates := map[string]interface{}{
			"status":     string(domain.TaskStatusProcessing),
			"started_at": &now,
		}

		if err := tx.Model(&model).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update task status: %w", err)
		}

		// 4. 重新加载更新后的模型
		if err := tx.Where("task_id = ?", taskID).First(&model).Error; err != nil {
			return fmt.Errorf("failed to reload task: %w", err)
		}

		// 5. 记录状态变更日志
		logEntry := TaskStatusLogModel{
			TaskID:     taskID,
			FromStatus: string(currentStatus),
			ToStatus:   string(domain.TaskStatusProcessing),
			Message:    "transitioned to processing",
		}
		if err := tx.Create(&logEntry).Error; err != nil {
			return fmt.Errorf("failed to create status log: %w", err)
		}

		result = model.ToDomain()
		return nil
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}
