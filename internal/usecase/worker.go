package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
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
	log := slog.With(
		"trace_id", logger.FromContext(ctx),
		"task_id", task.TaskID,
	)

	log.Info("Starting to process task")

	// 更新任务状态为 processing
	now := time.Now().Unix()
	task.Status = domain.TaskStatusProcessing
	task.StartedAt = &now
	if err := uc.repository.Update(ctx, task); err != nil {
		log.Error("Failed to update task status", "error", err)
		return fmt.Errorf("failed to update task status: %w", err)
	}

	// 创建临时工作目录
	workDir := filepath.Join(uc.tempDir, "cloud-media", task.TaskID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return uc.handleTaskError(ctx, task, "failed to create work dir", err)
	}
	defer func() {
		// 清理临时文件
		if err := os.RemoveAll(workDir); err != nil {
			log.Warn("Failed to cleanup work dir", "error", err)
		}
	}()

	// 1. 从 MinIO 下载源视频
	inputPath := filepath.Join(workDir, "input"+filepath.Ext(task.SourceKey))
	log.Info("Downloading source video", "source_bucket", task.SourceBucket, "source_key", task.SourceKey)

	if err := uc.downloadSourceVideo(ctx, task.SourceBucket, task.SourceKey, inputPath); err != nil {
		return uc.handleTaskError(ctx, task, "failed to download source video", err)
	}

	// 2. 获取视频信息并更新任务
	videoInfo, err := uc.transcoder.GetVideoInfo(ctx, inputPath)
	if err == nil {
		task.SourceDuration = videoInfo.Duration
		task.SourceSize = videoInfo.FileSize
		if err := uc.repository.Update(ctx, task); err != nil {
			log.Warn("Failed to update video info", "error", err)
		}
	}

	// 3. 执行转码
	outputDir := filepath.Join(workDir, "output")
	log.Info("Starting transcoding", "output_dir", outputDir)

	progressCallback := func(progress int, message string) {
		log.Debug("Transcoding progress", "progress", progress, "message", message)
		if err := uc.repository.UpdateProgress(ctx, task.TaskID, progress); err != nil {
			log.Warn("Failed to update progress", "error", err)
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
		return uc.handleTaskError(ctx, task, "failed to transcode video", err)
	}

	// 4. 上传转码结果到 MinIO
	log.Info("Uploading output files", "output_bucket", outputInfo.OutputBucket)
	if err := uc.uploadOutputFiles(ctx, outputDir, outputInfo); err != nil {
		return uc.handleTaskError(ctx, task, "failed to upload output files", err)
	}

	// 5. 更新任务为成功状态
	completedAt := time.Now().Unix()
	task.Status = domain.TaskStatusSuccess
	task.Progress = 100
	task.OutputInfo = outputInfo
	task.CompletedAt = &completedAt

	if err := uc.repository.Update(ctx, task); err != nil {
		log.Error("Failed to update task to success", "error", err)
		return fmt.Errorf("failed to update task: %w", err)
	}

	log.Info("Task completed successfully")
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
func (uc *WorkerUseCase) uploadOutputFiles(ctx context.Context, outputDir string, outputInfo *domain.OutputInfo) error {
	log := slog.With("trace_id", logger.FromContext(ctx))

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
		objectKey := filepath.Join(outputInfo.OutputBasePath, relPath)

		log.Debug("Uploading file", "path", path, "key", objectKey)

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", path, err)
		}
		defer file.Close()

		if err := uc.storage.UploadFromReader(ctx, outputInfo.OutputBucket, objectKey, file, info.Size()); err != nil {
			return fmt.Errorf("failed to upload %s: %w", path, err)
		}

		return nil
	})
}

// handleTaskError 处理任务错误
func (uc *WorkerUseCase) handleTaskError(ctx context.Context, task *domain.VideoTask, message string, err error) error {
	log := slog.With("trace_id", logger.FromContext(ctx))

	fullErr := fmt.Errorf("%s: %w", message, err)
	log.Error("Task failed", "error", fullErr)

	task.Status = domain.TaskStatusFailed
	task.ErrorMessage = fullErr.Error()

	if updateErr := uc.repository.Update(ctx, task); updateErr != nil {
		log.Error("Failed to update task to failed status", "error", updateErr)
	}

	return fullErr
}
