package transcoder

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/frozenf1sh/cloud-media/pkg/ffmpeg"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/frozenf1sh/cloud-media/pkg/metrics"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"github.com/google/wire"
)

// ProviderSet Wire 提供者集合
var ProviderSet = wire.NewSet(
	NewFFmpegTranscoder,
	wire.Bind(new(domain.Transcoder), new(*FFmpegTranscoder)),
)

// FFmpegTranscoder FFmpeg 转码器实现
type FFmpegTranscoder struct {
	ffmpeg          *ffmpeg.FFmpeg
	scaleCalculator *ffmpeg.ScaleCalculator
	progressParser  *ffmpeg.ProgressParser
	videoValidator  *ffmpeg.VideoValidator
	cfg             config.TranscoderConfig
}

// NewFFmpegTranscoder 创建 FFmpeg 转码器
func NewFFmpegTranscoder(cfg config.TranscoderConfig) (*FFmpegTranscoder, error) {
	f, err := ffmpeg.NewFFmpeg()
	if err != nil {
		return nil, err
	}

	vv, err := ffmpeg.NewDefaultVideoValidator()
	if err != nil {
		return nil, err
	}

	return &FFmpegTranscoder{
		ffmpeg:          f,
		scaleCalculator: ffmpeg.NewScaleCalculator(),
		progressParser:  ffmpeg.NewProgressParser(),
		videoValidator:  vv,
		cfg:             cfg,
	}, nil
}

// Transcode 执行视频转码为 HLS
func (t *FFmpegTranscoder) Transcode(
	ctx context.Context,
	inputPath string,
	outputDir string,
	config *domain.TranscodeConfig,
	videoInfo *domain.VideoInfo,
	onProgress domain.TranscodeProgressCallback,
) (*domain.OutputInfo, error) {
	ctx, span := telemetry.StartSpan(ctx, "FFmpegTranscoder.Transcode")
	defer span.End()

	log := slog.With(logger.String("trace_id", telemetry.TraceIDFromContext(ctx)))

	// 如果没有传入 videoInfo，则获取并验证
	if videoInfo == nil {
		var err error
		videoInfo, err = t.GetVideoInfo(ctx, inputPath)
		if err != nil {
			telemetry.RecordError(ctx, err)
			return nil, err
		}
	}

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		telemetry.RecordError(ctx, err)
		return nil, fmt.Errorf("failed to create output dir: %w", err)
	}

	outputBasePath := filepath.Base(outputDir)
	outputInfo := &domain.OutputInfo{
		OutputBasePath: outputBasePath,
		OutputBucket:   t.cfg.OutputBucket,
		Variants:       make([]domain.VariantOutput, 0, len(t.cfg.Variants)),
	}

	// 判断是否是竖屏视频
	isPortrait := videoInfo.Height > videoInfo.Width

	// 生成封面
	thumbnailPath := filepath.Join(outputDir, "thumbnail.jpg")
	if err := t.GenerateThumbnail(ctx, inputPath, thumbnailPath, 1.0, videoInfo); err == nil {
		outputInfo.ThumbnailPath = filepath.Join(outputBasePath, "thumbnail.jpg")
		log.InfoContext(ctx, "Generated thumbnail", logger.String("path", thumbnailPath))
	} else {
		log.WarnContext(ctx, "Failed to generate thumbnail", logger.Err(err))
		telemetry.RecordError(ctx, err)
	}

	// 计算进度权重
	numVariants := len(t.cfg.Variants)
	if numVariants == 0 {
		return nil, fmt.Errorf("no transcoding variants configured")
	}
	variantProgressWeight := 90 / numVariants // 封面占 10%，变体共享 90%

	// 转码每个变体
	for i, variant := range t.cfg.Variants {
		// 计算保持宽高比的分辨率
		scaleWidth, scaleHeight := t.scaleCalculator.Calculate(videoInfo.Width, videoInfo.Height, variant.TargetSize)
		resolutionStr := fmt.Sprintf("%dx%d", scaleWidth, scaleHeight)

		variantDir := filepath.Join(outputDir, variant.Name)
		if err := os.MkdirAll(variantDir, 0755); err != nil {
			telemetry.RecordError(ctx, err)
			return nil, fmt.Errorf("failed to create variant dir: %w", err)
		}

		playlistPath := filepath.Join(variantDir, "index.m3u8")
		segmentPattern := filepath.Join(variantDir, "segment_%04d.ts")

		// 计算进度
		progressOffset := 10 + i*variantProgressWeight
		variantCallback := func(progress int, message string) {
			if onProgress != nil {
				totalProgress := progressOffset + (progress * variantProgressWeight / 100)
				onProgress(totalProgress, message)
			}
		}

		// 执行转码
		if err := t.transcodeVariant(ctx, inputPath, playlistPath, segmentPattern, scaleWidth, scaleHeight, variant.Bitrate, videoInfo, variantCallback); err != nil {
			telemetry.RecordError(ctx, err)
			return nil, fmt.Errorf("failed to transcode %s: %w", variant.Name, err)
		}

		metrics.RecordTranscodedVideo(variant.Name)

		// 记录变体信息
		variantResolution := resolutionStr
		if isPortrait {
			variantResolution = fmt.Sprintf("%dp (portrait)", variant.TargetSize)
		} else {
			variantResolution = fmt.Sprintf("%dp", variant.TargetSize)
		}
		outputInfo.Variants = append(outputInfo.Variants, domain.VariantOutput{
			Resolution:   variantResolution,
			PlaylistPath: filepath.Join(outputBasePath, variant.Name, "index.m3u8"),
			Bandwidth:    variant.Bandwidth,
		})
	}

	// 生成主播放列表
	masterPlaylistPath := filepath.Join(outputDir, "master.m3u8")
	if err := t.generateMasterPlaylist(masterPlaylistPath, outputInfo.Variants, videoInfo); err != nil {
		telemetry.RecordError(ctx, err)
		return nil, fmt.Errorf("failed to generate master playlist: %w", err)
	}
	outputInfo.PlaylistPath = filepath.Join(outputBasePath, "master.m3u8")

	// 完成
	if onProgress != nil {
		onProgress(100, "Transcoding completed")
	}

	// 设置 span 状态为成功
	telemetry.SetSpanStatusOK(ctx)

	return outputInfo, nil
}

// transcodeVariant 转码单个码率变体
func (t *FFmpegTranscoder) transcodeVariant(
	ctx context.Context,
	inputPath string,
	playlistPath string,
	segmentPattern string,
	width int,
	height int,
	bitrate string,
	videoInfo *domain.VideoInfo,
	onProgress domain.TranscodeProgressCallback,
) error {
	ctx, span := telemetry.StartSpan(ctx, "FFmpegTranscoder.transcodeVariant",
		telemetry.String("resolution", fmt.Sprintf("%dx%d", width, height)),
		telemetry.String("bitrate", bitrate),
	)
	defer span.End()

	log := slog.With(logger.String("trace_id", telemetry.TraceIDFromContext(ctx)))

	// 计算超时时间
	timeout := time.Duration(videoInfo.Duration*t.cfg.TimeoutMultiplier) * time.Second
	if timeout < time.Duration(t.cfg.MinTimeout)*time.Minute {
		timeout = time.Duration(t.cfg.MinTimeout) * time.Minute
	}
	if timeout > time.Duration(t.cfg.MaxTimeout)*time.Minute {
		timeout = time.Duration(t.cfg.MaxTimeout) * time.Minute
	}

	// 创建带超时的 context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	scaleFilter := t.scaleCalculator.ScaleFilter(width, height)

	args := []string{
		"-y",
		"-i", inputPath,
		"-vf", scaleFilter,
		"-c:v", t.cfg.VideoCodec,
		"-b:v", bitrate,
		"-preset", t.cfg.Preset,
		"-g", fmt.Sprintf("%d", t.cfg.GOPSize),
		"-sc_threshold", "0",
		"-c:a", t.cfg.AudioCodec,
		"-b:a", t.cfg.AudioBitrate,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", t.cfg.HLSTime),
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	}

	cmd := t.ffmpeg.Command(timeoutCtx, args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		telemetry.RecordError(ctx, err)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		telemetry.RecordError(ctx, err)
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// 解析进度
	go t.progressParser.Parse(stderr, videoInfo.Duration, ffmpeg.ProgressCallback(onProgress))

	if err := cmd.Wait(); err != nil {
		// 检查是否是超时导致的
		if timeoutCtx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("transcoding timed out after %v", timeout)
		}
		telemetry.RecordError(ctx, err)
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	log.InfoContext(ctx, "Variant transcoded",
		logger.String("resolution", fmt.Sprintf("%dx%d", width, height)),
		logger.String("bitrate", bitrate),
	)
	return nil
}

// generateMasterPlaylist 生成 HLS 主播放列表
func (t *FFmpegTranscoder) generateMasterPlaylist(path string, variants []domain.VariantOutput, videoInfo *domain.VideoInfo) error {
	var sb strings.Builder

	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")

	for i, variant := range variants {
		// 计算该变体的实际分辨率
		width, height := t.scaleCalculator.Calculate(videoInfo.Width, videoInfo.Height, t.cfg.Variants[i].TargetSize)
		sb.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n",
			variant.Bandwidth, width, height))
		sb.WriteString(fmt.Sprintf("%s/index.m3u8\n", t.cfg.Variants[i].Name))
	}

	return os.WriteFile(path, []byte(sb.String()), 064)
}

// GetVideoInfo 获取并验证视频信息
func (t *FFmpegTranscoder) GetVideoInfo(ctx context.Context, inputPath string) (*domain.VideoInfo, error) {
	ctx, span := telemetry.StartSpan(ctx, "FFmpegTranscoder.GetVideoInfo")
	defer span.End()

	// 验证输入文件，同时获取视频信息
	ffmpegVideoInfo, err := t.videoValidator.ValidateAndGetInfo(ctx, inputPath)
	if err != nil {
		telemetry.RecordError(ctx, err)
		return nil, fmt.Errorf("invalid video file: %w", err)
	}

	// 转换为 domain.VideoInfo
	return &domain.VideoInfo{
		Duration:   ffmpegVideoInfo.Duration,
		Width:      ffmpegVideoInfo.Width,
		Height:     ffmpegVideoInfo.Height,
		Codec:      ffmpegVideoInfo.Codec,
		Bitrate:    ffmpegVideoInfo.Bitrate,
		FPS:        ffmpegVideoInfo.FPS,
		AudioCodec: ffmpegVideoInfo.AudioCodec,
		FileSize:   ffmpegVideoInfo.FileSize,
	}, nil
}

// GenerateThumbnail 生成视频封面，保持宽高比缩放
func (t *FFmpegTranscoder) GenerateThumbnail(ctx context.Context, inputPath string, outputPath string, timeOffset float64, videoInfo *domain.VideoInfo) error {
	ctx, span := telemetry.StartSpan(ctx, "FFmpegTranscoder.GenerateThumbnail")
	defer span.End()

	var args []string

	if videoInfo != nil && videoInfo.Width > 0 && videoInfo.Height > 0 {
		// 计算封面缩放后的尺寸
		thumbWidth, thumbHeight := t.scaleCalculator.Calculate(videoInfo.Width, videoInfo.Height, t.cfg.ThumbnailSize)
		scaleFilter := t.scaleCalculator.ScaleFilter(thumbWidth, thumbHeight)

		args = []string{
			"-y",
			"-ss", fmt.Sprintf("%.2f", timeOffset),
			"-i", inputPath,
			"-vf", scaleFilter,
			"-vframes", "1",
			"-q:v", "2",
			outputPath,
		}
	} else {
		// 没有视频信息时使用原始尺寸
		args = []string{
			"-y",
			"-ss", fmt.Sprintf("%.2f", timeOffset),
			"-i", inputPath,
			"-vframes", "1",
			"-q:v", "2",
			outputPath,
		}
	}

	return t.ffmpeg.Run(ctx, args...)
}

// Cleanup 清理临时文件
func (t *FFmpegTranscoder) Cleanup(dir string) error {
	return os.RemoveAll(dir)
}
