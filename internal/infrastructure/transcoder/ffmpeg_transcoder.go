// Package transcoder 实现视频转码器，使用 FFmpeg 进行 HLS 切片。
//
// 特性：
//   - 多码率变体输出（1080p、720p、480p）
//   - 单进程多路输出优化
//   - 自动封面生成
//   - 竖屏视频支持
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

	// 记录视频原始信息（debug）
	log.InfoContext(ctx, "Video info for transcoding",
		logger.Int("original_width", videoInfo.Width),
		logger.Int("original_height", videoInfo.Height),
		logger.Int("rotation", videoInfo.Rotation),
		logger.Float64("duration", videoInfo.Duration),
		logger.String("codec", videoInfo.Codec),
	)

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

	// 判断是否是竖屏视频（考虑 rotation）
	effectiveWidth, effectiveHeight := ffmpeg.GetEffectiveDimensions(videoInfo.Width, videoInfo.Height, videoInfo.Rotation)
	isPortrait := effectiveHeight > effectiveWidth

	log.InfoContext(ctx, "Effective video dimensions",
		logger.Int("effective_width", effectiveWidth),
		logger.Int("effective_height", effectiveHeight),
		logger.Bool("is_portrait", isPortrait),
	)

	// 生成封面
	thumbnailPath := filepath.Join(outputDir, "thumbnail.jpg")
	if err := t.GenerateThumbnail(ctx, inputPath, thumbnailPath, 1.0, videoInfo); err == nil {
		outputInfo.ThumbnailPath = filepath.Join(outputBasePath, "thumbnail.jpg")
		log.InfoContext(ctx, "Generated thumbnail", logger.String("path", thumbnailPath))
		if onProgress != nil {
			onProgress(10, "Thumbnail generated")
		}
	} else {
		log.WarnContext(ctx, "Failed to generate thumbnail", logger.Err(err))
		telemetry.RecordError(ctx, err)
	}

	// 检查是否配置了变体
	numVariants := len(t.cfg.Variants)
	if numVariants == 0 {
		return nil, fmt.Errorf("no transcoding variants configured")
	}

	// 准备变体输出信息和目录
	variantConfigs := make([]variantOutputConfig, 0, numVariants)
	for _, variant := range t.cfg.Variants {
		scaleWidth, scaleHeight := t.scaleCalculator.CalculateWithRotation(videoInfo.Width, videoInfo.Height, variant.TargetSize, videoInfo.Rotation)
		variantDir := filepath.Join(outputDir, variant.Name)

		if err := os.MkdirAll(variantDir, 0755); err != nil {
			telemetry.RecordError(ctx, err)
			return nil, fmt.Errorf("failed to create variant dir: %w", err)
		}

		variantConfigs = append(variantConfigs, variantOutputConfig{
			variant:         variant,
			width:           scaleWidth,
			height:          scaleHeight,
			variantDir:      variantDir,
			playlistPath:    filepath.Join(variantDir, "index.m3u8"),
			segmentPattern:  filepath.Join(variantDir, "segment_%04d.ts"),
			outputBasePath:  outputBasePath,
			isPortrait:      isPortrait,
		})

		log.InfoContext(ctx, "Calculated variant dimensions",
			logger.String("variant", variant.Name),
			logger.Int("target_size", variant.TargetSize),
			logger.Int("output_width", scaleWidth),
			logger.Int("output_height", scaleHeight),
		)
	}

	// 构建进度回调：封面10%，转码90%
	progressCallback := func(progress int, message string) {
		if onProgress != nil {
			totalProgress := 10 + (progress * 90 / 100)
			onProgress(totalProgress, message)
		}
	}

	// 一次性转码所有变体（单进程多路输出）
	if err := t.transcodeAllVariants(ctx, inputPath, videoInfo, variantConfigs, progressCallback); err != nil {
		telemetry.RecordError(ctx, err)
		return nil, fmt.Errorf("failed to transcode variants: %w", err)
	}

	// 记录变体信息到 outputInfo
	for _, vc := range variantConfigs {
		var variantResolution string
		if vc.isPortrait {
			variantResolution = fmt.Sprintf("%dp (portrait)", vc.variant.TargetSize)
		} else {
			variantResolution = fmt.Sprintf("%dp", vc.variant.TargetSize)
		}
		outputInfo.Variants = append(outputInfo.Variants, domain.VariantOutput{
			Resolution:   variantResolution,
			PlaylistPath: filepath.Join(vc.outputBasePath, vc.variant.Name, "index.m3u8"),
			Bandwidth:    vc.variant.Bandwidth,
		})

		metrics.RecordTranscodedVideo(vc.variant.Name)
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

// variantOutputConfig 单个变体的输出配置
type variantOutputConfig struct {
	variant        config.TranscoderVariantConfig
	width          int
	height         int
	variantDir     string
	playlistPath   string
	segmentPattern string
	outputBasePath string
	isPortrait     bool
}

// transcodeAllVariants 使用单进程多路输出转码所有变体
func (t *FFmpegTranscoder) transcodeAllVariants(
	ctx context.Context,
	inputPath string,
	videoInfo *domain.VideoInfo,
	variantConfigs []variantOutputConfig,
	onProgress domain.TranscodeProgressCallback,
) error {
	ctx, span := telemetry.StartSpan(ctx, "FFmpegTranscoder.transcodeAllVariants",
		telemetry.Int("num_variants", len(variantConfigs)),
	)
	defer span.End()

	log := slog.With(logger.String("trace_id", telemetry.TraceIDFromContext(ctx)))

	// 计算超时时间（复用原逻辑）
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

	// 构建 filtergraph
	filtergraph, outputLabels, err := t.buildFiltergraph(videoInfo, variantConfigs)
	if err != nil {
		telemetry.RecordError(ctx, err)
		return fmt.Errorf("failed to build filtergraph: %w", err)
	}

	log.InfoContext(ctx, "Built filtergraph",
		logger.String("filtergraph", filtergraph),
		logger.Int("num_outputs", len(outputLabels)),
	)

	// 构建完整命令参数
	args := []string{
		"-y",
		"-noautorotate",
	}

	// 添加线程数限制（如果配置了）
	if t.cfg.ThreadCount > 0 {
		args = append(args, "-threads", fmt.Sprintf("%d", t.cfg.ThreadCount))
	}

	args = append(args,
		"-i", inputPath,
		"-filter_complex", filtergraph,
	)

	// 添加每个输出的参数
	for i, vc := range variantConfigs {
		outputArgs := t.buildOutputArgs(vc, outputLabels[i])
		args = append(args, outputArgs...)
	}

	// 记录完整的 FFmpeg 命令用于调试
	cmdStr := ffmpeg.FormatCommand(args)
	log.InfoContext(ctx, "FFmpeg multi-output command",
		logger.String("command", cmdStr),
	)

	// 执行命令
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
		return fmt.Errorf("ffmpeg failed with command [%s]: %w", cmdStr, err)
	}

	log.InfoContext(ctx, "All variants transcoded successfully")
	return nil
}

// buildFiltergraph 构建复杂滤镜图
// 返回: filtergraph 字符串、各变体的输出标签名
func (t *FFmpegTranscoder) buildFiltergraph(
	videoInfo *domain.VideoInfo,
	variantConfigs []variantOutputConfig,
) (string, []string, error) {
	numVariants := len(variantConfigs)
	if numVariants == 0 {
		return "", nil, fmt.Errorf("no variants provided")
	}

	var parts []string

	// 第一步：构建输入处理链
	var currentInputLabel = "0:v"

	// 添加旋转滤镜（如需要）
	// 注意：FFmpeg 的 transpose 滤镜方向与 rotation metadata 相反
	rotationFilter, newLabel := ffmpeg.ApplyRotationToLabel(currentInputLabel, videoInfo.Rotation)
	if rotationFilter != "" {
		parts = append(parts, rotationFilter)
		currentInputLabel = newLabel
	}

	// 添加 split 滤镜
	splitLabels := make([]string, numVariants)
	for i := 0; i < numVariants; i++ {
		splitLabels[i] = fmt.Sprintf("[v%d]", i+1)
	}
	parts = append(parts, fmt.Sprintf("[%s]split=%d%s", currentInputLabel, numVariants, strings.Join(splitLabels, "")))

	// 第二步：为每个变体添加缩放链
	outputLabels := make([]string, numVariants)
	for i, vc := range variantConfigs {
		outputLabels[i] = fmt.Sprintf("out%d", i+1)
		parts = append(parts, fmt.Sprintf("%sscale=%d:%d:flags=lanczos,setsar=1:1[%s]",
			splitLabels[i], vc.width, vc.height, outputLabels[i]))
	}

	// 组合完整 filtergraph
	return strings.Join(parts, ";"), outputLabels, nil
}

// buildOutputArgs 构建单个输出的参数
func (t *FFmpegTranscoder) buildOutputArgs(
	vc variantOutputConfig,
	outputLabel string,
) []string {
	return []string{
		"-map", fmt.Sprintf("[%s]", outputLabel),
		"-map", "0:a",
		"-c:v", t.cfg.VideoCodec,
		"-b:v", vc.variant.Bitrate,
		"-preset", t.cfg.Preset,
		"-g", fmt.Sprintf("%d", t.cfg.GOPSize),
		"-sc_threshold", "0",
		"-c:a", t.cfg.AudioCodec,
		"-b:a", t.cfg.AudioBitrate,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", t.cfg.HLSTime),
		"-hls_list_size", "0",
		"-hls_segment_filename", vc.segmentPattern,
		vc.playlistPath,
	}
}

// generateMasterPlaylist 生成 HLS 主播放列表
func (t *FFmpegTranscoder) generateMasterPlaylist(path string, variants []domain.VariantOutput, videoInfo *domain.VideoInfo) error {
	hlsVariants := make([]ffmpeg.VariantConfig, len(variants))
	for i, variant := range variants {
		width, height := t.scaleCalculator.CalculateWithRotation(videoInfo.Width, videoInfo.Height, t.cfg.Variants[i].TargetSize, videoInfo.Rotation)
		hlsVariants[i] = ffmpeg.VariantConfig{
			Name:      t.cfg.Variants[i].Name,
			Width:     width,
			Height:    height,
			Bandwidth: variant.Bandwidth,
		}
	}
	return ffmpeg.WriteMasterPlaylist(path, hlsVariants)
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
		Rotation:   ffmpegVideoInfo.Rotation,
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

	log := slog.With(logger.String("trace_id", telemetry.TraceIDFromContext(ctx)))

	var args []string

	if videoInfo != nil && videoInfo.Width > 0 && videoInfo.Height > 0 {
		// 计算封面缩放后的尺寸（考虑 rotation）
		thumbWidth, thumbHeight := t.scaleCalculator.CalculateWithRotation(videoInfo.Width, videoInfo.Height, t.cfg.ThumbnailSize, videoInfo.Rotation)

		// 构建视频滤镜链：先旋转（如果需要），再缩放
		var filters []string
		rotationFilter := ffmpeg.BuildRotationFilter(videoInfo.Rotation)
		if rotationFilter != "" {
			filters = append(filters, rotationFilter)
		}
		filters = append(filters, fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1:1", thumbWidth, thumbHeight))
		vfFilter := strings.Join(filters, ",")

		// 记录缩略图生成参数（debug）
		log.InfoContext(ctx, "Generating thumbnail with parameters",
			logger.Int("original_width", videoInfo.Width),
			logger.Int("original_height", videoInfo.Height),
			logger.Int("rotation", videoInfo.Rotation),
			logger.Int("thumb_width", thumbWidth),
			logger.Int("thumb_height", thumbHeight),
			logger.String("vf_filter", vfFilter),
		)

		args = []string{
			"-y",
			"-noautorotate", // 禁用自动旋转
		}

		// 添加线程数限制（如果配置了）
		if t.cfg.ThreadCount > 0 {
			args = append(args, "-threads", fmt.Sprintf("%d", t.cfg.ThreadCount))
		}

		args = append(args,
			"-ss", fmt.Sprintf("%.2f", timeOffset),
			"-i", inputPath,
			"-vf", vfFilter,
			"-vframes", "1",
			"-q:v", "2",
			outputPath,
		)
	} else {
		log.InfoContext(ctx, "Generating thumbnail without video info")
		// 没有视频信息时直接提取一帧
		args = []string{
			"-y",
			"-noautorotate",
		}

		// 添加线程数限制（如果配置了）
		if t.cfg.ThreadCount > 0 {
			args = append(args, "-threads", fmt.Sprintf("%d", t.cfg.ThreadCount))
		}

		args = append(args,
			"-ss", fmt.Sprintf("%.2f", timeOffset),
			"-i", inputPath,
			"-vframes", "1",
			"-q:v", "2",
			outputPath,
		)
	}

	return t.ffmpeg.Run(ctx, args...)
}

// Cleanup 清理临时文件
func (t *FFmpegTranscoder) Cleanup(dir string) error {
	return os.RemoveAll(dir)
}
