package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/frozenf1sh/cloud-media/pkg/metrics"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"github.com/google/wire"
)

// WorkerProviderSet Worker 相关的 Wire 提供者集合
var WorkerProviderSet = wire.NewSet(NewWorkerUseCase)

// WorkerUseCase Worker 业务用例
type WorkerUseCase struct {
	repository   domain.VideoTaskRepository
	transcoder   domain.Transcoder
	storage      domain.ObjectStorage
	tempDir      string
}

// NewWorkerUseCase 创建 WorkerUseCase
func NewWorkerUseCase(
	repo domain.VideoTaskRepository,
	transcoder domain.Transcoder,
	storage domain.ObjectStorage,
) *WorkerUseCase {
	return &WorkerUseCase{
		repository: repo,
		transcoder: transcoder,
		storage:    storage,
		tempDir:    os.TempDir(),
	}
}

// ProcessTask 处理单个视频转码任务
func (uc *WorkerUseCase) ProcessTask(ctx context.Context, task *domain.VideoTask) error {
	ctx, span := telemetry.StartSpan(ctx, "WorkerUseCase.ProcessTask",
		telemetry.String("task_id", task.TaskID),
	)
	defer span.End()

	metrics.RecordTaskStarted()

	startTime := time.Now()

	logger.InfoContext(ctx, "Starting to process task",
		logger.String("task_id", task.TaskID))

	// 更新任务状态为 processing
	now := time.Now().Unix()
	task.Status = domain.TaskStatusProcessing
	task.StartedAt = &now
	if err := uc.repository.Update(ctx, task); err != nil {
		logger.ErrorContext(ctx, "Failed to update task status",
			logger.Err(err),
			logger.String("task_id", task.TaskID))
		telemetry.RecordError(ctx, err)
		metrics.RecordTaskCompleted("failed", time.Since(startTime))
		return fmt.Errorf("failed to update task status: %w", err)
	}

	// 创建临时工作目录
	workDir := filepath.Join(uc.tempDir, "cloud-media", task.TaskID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		telemetry.RecordError(ctx, err)
		metrics.RecordTaskCompleted("failed", time.Since(startTime))
		return uc.handleTaskError(ctx, task, "failed to create work dir", err)
	}
	defer func() {
		// 清理临时文件
		if err := os.RemoveAll(workDir); err != nil {
			logger.WarnContext(ctx, "Failed to cleanup work dir",
				logger.Err(err),
				logger.String("task_id", task.TaskID))
		}
	}()

	// 1. 从 MinIO 下载源视频
	inputPath := filepath.Join(workDir, "input"+filepath.Ext(task.SourceKey))
	logger.InfoContext(ctx, "Downloading source video",
		logger.String("task_id", task.TaskID),
		logger.String("source_bucket", task.SourceBucket),
		logger.String("source_key", task.SourceKey))

	if err := uc.downloadSourceVideo(ctx, task.SourceBucket, task.SourceKey, inputPath); err != nil {
		telemetry.RecordError(ctx, err)
		metrics.RecordTaskCompleted("failed", time.Since(startTime))
		return uc.handleTaskError(ctx, task, "failed to download source video", err)
	}

	// 2. 获取视频信息并更新任务
	videoInfo, err := uc.transcoder.GetVideoInfo(ctx, inputPath)
	if err == nil {
		task.SourceDuration = videoInfo.Duration
		task.SourceSize = videoInfo.FileSize
		if err := uc.repository.Update(ctx, task); err != nil {
			logger.WarnContext(ctx, "Failed to update video info",
				logger.Err(err),
				logger.String("task_id", task.TaskID))
		}
	}

	// 3. 执行转码
	outputDir := filepath.Join(workDir, "output")
	logger.InfoContext(ctx, "Starting transcoding",
		logger.String("task_id", task.TaskID),
		logger.String("output_dir", outputDir))

	progressCallback := func(progress int, message string) {
		logger.DebugContext(ctx, "Transcoding progress",
			logger.String("task_id", task.TaskID),
			logger.Int("progress", progress),
			logger.String("message", message))
		if err := uc.repository.UpdateProgress(ctx, task.TaskID, progress); err != nil {
			logger.WarnContext(ctx, "Failed to update progress",
				logger.Err(err),
				logger.String("task_id", task.TaskID))
		}
	}

	var transcodeConfig *domain.TranscodeConfig
	if task.TranscodeConfig != nil {
		transcodeConfig = task.TranscodeConfig
	} else {
		// 默认配置
		transcodeConfig = &domain.TranscodeConfig{
			OutputFormat: "hls",
			Video: domain.VideoTranscodeConfig{
				Codec: "libx264",
			},
			Audio: domain.AudioTranscodeConfig{
				Codec: "aac",
			},
		}
	}

	outputInfo, err := uc.transcoder.Transcode(ctx, inputPath, outputDir, transcodeConfig, progressCallback)
	if err != nil {
		telemetry.RecordError(ctx, err)
		metrics.RecordTaskCompleted("failed", time.Since(startTime))
		return uc.handleTaskError(ctx, task, "failed to transcode video", err)
	}

	// 修正输出路径 - 使用 taskID 作为 base path
	outputInfo.OutputBasePath = task.TaskID
	if outputInfo.PlaylistPath != "" {
		outputInfo.PlaylistPath = filepath.Join(task.TaskID, "master.m3u8")
	}
	if outputInfo.ThumbnailPath != "" {
		outputInfo.ThumbnailPath = filepath.Join(task.TaskID, "thumbnail.jpg")
	}
	for i := range outputInfo.Variants {
		oldPath := outputInfo.Variants[i].PlaylistPath
		// 从旧路径中提取 resolution 部分
		parts := strings.Split(oldPath, string(filepath.Separator))
		if len(parts) >= 2 {
			resolution := parts[len(parts)-2]
			outputInfo.Variants[i].PlaylistPath = filepath.Join(task.TaskID, resolution, "index.m3u8")
		}
	}

	// 4. 上传转码结果到 MinIO
	logger.InfoContext(ctx, "Uploading output files",
		logger.String("task_id", task.TaskID),
		logger.String("output_bucket", outputInfo.OutputBucket),
		logger.String("base_path", task.TaskID))
	if err := uc.uploadOutputFiles(ctx, outputDir, outputInfo, task.TaskID); err != nil {
		telemetry.RecordError(ctx, err)
		metrics.RecordTaskCompleted("failed", time.Since(startTime))
		return uc.handleTaskError(ctx, task, "failed to upload output files", err)
	}

	// 5. 更新任务为成功状态
	completedAt := time.Now().Unix()
	task.Status = domain.TaskStatusSuccess
	task.Progress = 100
	task.OutputInfo = outputInfo
	task.CompletedAt = &completedAt

	if err := uc.repository.Update(ctx, task); err != nil {
		logger.ErrorContext(ctx, "Failed to update task to success",
			logger.Err(err),
			logger.String("task_id", task.TaskID))
		telemetry.RecordError(ctx, err)
		metrics.RecordTaskCompleted("failed", time.Since(startTime))
		return fmt.Errorf("failed to update task: %w", err)
	}

	logger.InfoContext(ctx, "Task completed successfully",
		logger.String("task_id", task.TaskID))
	metrics.RecordTaskCompleted("success", time.Since(startTime))

	// 设置 span 状态为成功
	telemetry.SetSpanStatusOK(ctx)
	return nil
}

// downloadSourceVideo 从存储下载源视频
func (uc *WorkerUseCase) downloadSourceVideo(ctx context.Context, bucket, key, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	if err := uc.storage.DownloadToWriter(ctx, bucket, key, file); err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	return nil
}

// uploadOutputFiles 上传输出文件到存储
func (uc *WorkerUseCase) uploadOutputFiles(ctx context.Context, outputDir string, outputInfo *domain.OutputInfo, basePath string) error {
	// 遍历输出目录上传所有文件
	return filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// 计算相对路径作为 object key
		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		objectKey := filepath.Join(basePath, relPath)

		logger.DebugContext(ctx, "Uploading file",
			logger.String("path", path),
			logger.String("key", objectKey))

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", path, err)
		}
		defer file.Close()

		if err := uc.storage.UploadFromReader(ctx, outputInfo.OutputBucket, objectKey, file, info.Size()); err != nil {
			return fmt.Errorf("failed to upload %s: %w", path, err)
		}

		metrics.RecordTranscodedBytes(info.Size())

		return nil
	})
}

// handleTaskError 处理任务错误
func (uc *WorkerUseCase) handleTaskError(ctx context.Context, task *domain.VideoTask, message string, err error) error {
	fullErr := fmt.Errorf("%s: %w", message, err)
	logger.ErrorContext(ctx, "Task failed",
		logger.Err(fullErr),
		logger.String("task_id", task.TaskID))

	task.Status = domain.TaskStatusFailed
	task.ErrorMessage = fullErr.Error()

	if updateErr := uc.repository.Update(ctx, task); updateErr != nil {
		logger.ErrorContext(ctx, "Failed to update task to failed status",
			logger.Err(updateErr),
			logger.String("task_id", task.TaskID))
	}

	return fullErr
}
