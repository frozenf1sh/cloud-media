package transcoder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/google/wire"
)

// ProviderSet Wire 提供者集合
var ProviderSet = wire.NewSet(
	NewFFmpegTranscoder,
	wire.Bind(new(domain.Transcoder), new(*FFmpegTranscoder)),
)

// FFmpegTranscoder FFmpeg 转码器实现
type FFmpegTranscoder struct {
	ffmpegPath  string
	ffprobePath string
}

// NewFFmpegTranscoder 创建 FFmpeg 转码器
func NewFFmpegTranscoder() (*FFmpegTranscoder, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found: %w", err)
	}

	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %w", err)
	}

	return &FFmpegTranscoder{
		ffmpegPath:  ffmpegPath,
		ffprobePath: ffprobePath,
	}, nil
}

// Transcode 执行视频转码为 HLS
func (t *FFmpegTranscoder) Transcode(
	ctx context.Context,
	inputPath string,
	outputDir string,
	config *domain.TranscodeConfig,
	onProgress domain.TranscodeProgressCallback,
) (*domain.OutputInfo, error) {
	log := slog.With("trace_id", logger.FromContext(ctx))

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output dir: %w", err)
	}

	// 获取视频信息
	videoInfo, err := t.GetVideoInfo(ctx, inputPath)
	if err != nil {
		log.Warn("Failed to get video info, using defaults", "error", err)
		videoInfo = &domain.VideoInfo{Duration: 0}
	}

	// 定义多码率变体
	variants := []struct {
		resolution string
		bitrate    string
		bandwidth  int
	}{
		{"1920x1080", "4000k", 4000000},
		{"1280x720", "2000k", 2000000},
		{"854x480", "1000k", 1000000},
	}

	outputBasePath := filepath.Base(outputDir)
	outputBucket := "media-output" // TODO: 从配置获取
	outputInfo := &domain.OutputInfo{
		OutputBasePath: outputBasePath,
		OutputBucket:   outputBucket,
		Variants:       make([]domain.VariantOutput, 0, len(variants)),
	}

	// 生成封面
	thumbnailPath := filepath.Join(outputDir, "thumbnail.jpg")
	if err := t.GenerateThumbnail(ctx, inputPath, thumbnailPath, 1.0); err == nil {
		outputInfo.ThumbnailPath = filepath.Join(outputBasePath, "thumbnail.jpg")
		log.Info("Generated thumbnail", "path", thumbnailPath)
	} else {
		log.Warn("Failed to generate thumbnail", "error", err)
	}

	// 转码每个变体
	for i, variant := range variants {
		variantDir := filepath.Join(outputDir, variant.resolution)
		if err := os.MkdirAll(variantDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create variant dir: %w", err)
		}

		playlistPath := filepath.Join(variantDir, "index.m3u8")
		segmentPattern := filepath.Join(variantDir, "segment_%04d.ts")

		// 计算进度权重（每个变体占 30%，封面占 10%）
		progressOffset := 10 + i*30
		variantCallback := func(progress int, message string) {
			if onProgress != nil {
				totalProgress := progressOffset + (progress * 30 / 100)
				onProgress(totalProgress, message)
			}
		}

		// 执行转码
		if err := t.transcodeVariant(ctx, inputPath, playlistPath, segmentPattern, variant.resolution, variant.bitrate, videoInfo, variantCallback); err != nil {
			return nil, fmt.Errorf("failed to transcode %s: %w", variant.resolution, err)
		}

		// 记录变体信息
		outputInfo.Variants = append(outputInfo.Variants, domain.VariantOutput{
			Resolution:   variant.resolution,
			PlaylistPath: filepath.Join(outputBasePath, variant.resolution, "index.m3u8"),
			Bandwidth:    variant.bandwidth,
		})
	}

	// 生成主播放列表
	masterPlaylistPath := filepath.Join(outputDir, "master.m3u8")
	if err := t.generateMasterPlaylist(masterPlaylistPath, outputInfo.Variants); err != nil {
		return nil, fmt.Errorf("failed to generate master playlist: %w", err)
	}
	outputInfo.PlaylistPath = filepath.Join(outputBasePath, "master.m3u8")

	// 完成
	if onProgress != nil {
		onProgress(100, "Transcoding completed")
	}

	return outputInfo, nil
}

// transcodeVariant 转码单个码率变体
func (t *FFmpegTranscoder) transcodeVariant(
	ctx context.Context,
	inputPath string,
	playlistPath string,
	segmentPattern string,
	resolution string,
	bitrate string,
	videoInfo *domain.VideoInfo,
	onProgress domain.TranscodeProgressCallback,
) error {
	log := slog.With("trace_id", logger.FromContext(ctx))

	args := []string{
		"-y",
		"-i", inputPath,
		"-vf", fmt.Sprintf("scale=%s:flags=lanczos", resolution),
		"-c:v", "libx264",
		"-b:v", bitrate,
		"-preset", "fast",
		"-g", "48",
		"-sc_threshold", "0",
		"-c:a", "aac",
		"-b:a", "128k",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	}

	cmd := exec.CommandContext(ctx, t.ffmpegPath, args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// 解析进度
	go t.parseProgress(stderr, videoInfo.Duration, onProgress)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	log.Info("Variant transcoded", "resolution", resolution, "bitrate", bitrate)
	return nil
}

// parseProgress 解析 FFmpeg 输出进度
func (t *FFmpegTranscoder) parseProgress(stderr io.ReadCloser, duration float64, onProgress domain.TranscodeProgressCallback) {
	scanner := bufio.NewScanner(stderr)
	timeRegex := regexp.MustCompile(`time=(\d{2}):(\d{2}):(\d{2})\.(\d{2})`)

	for scanner.Scan() {
		line := scanner.Text()

		if matches := timeRegex.FindStringSubmatch(line); matches != nil {
			hours, _ := strconv.Atoi(matches[1])
			minutes, _ := strconv.Atoi(matches[2])
			seconds, _ := strconv.Atoi(matches[3])
			csec, _ := strconv.Atoi(matches[4])

			currentTime := float64(hours*3600+minutes*60+seconds) + float64(csec)/100.0

			if duration > 0 {
				progress := int((currentTime / duration) * 100)
				if progress > 100 {
					progress = 100
				}
				if onProgress != nil {
					onProgress(progress, fmt.Sprintf("Processing: %.1f/%.1fs", currentTime, duration))
				}
			}
		}
	}
}

// generateMasterPlaylist 生成 HLS 主播放列表
func (t *FFmpegTranscoder) generateMasterPlaylist(path string, variants []domain.VariantOutput) error {
	var sb strings.Builder

	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")

	for _, variant := range variants {
		width, height := t.parseResolution(variant.Resolution)
		sb.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n",
			variant.Bandwidth, width, height))
		sb.WriteString(fmt.Sprintf("%s\n", variant.Resolution+"/index.m3u8"))
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// parseResolution 解析分辨率字符串
func (t *FFmpegTranscoder) parseResolution(res string) (int, int) {
	parts := strings.Split(res, "x")
	if len(parts) != 2 {
		return 1920, 1080
	}
	width, _ := strconv.Atoi(parts[0])
	height, _ := strconv.Atoi(parts[1])
	return width, height
}

// GetVideoInfo 获取视频信息
func (t *FFmpegTranscoder) GetVideoInfo(ctx context.Context, inputPath string) (*domain.VideoInfo, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	}

	cmd := exec.CommandContext(ctx, t.ffprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	// 简单解析（实际项目建议使用完整的 JSON 结构）
	return t.parseFFprobeOutput(output)
}

// parseFFprobeOutput 解析 ffprobe 输出
func (t *FFmpegTranscoder) parseFFprobeOutput(data []byte) (*domain.VideoInfo, error) {
	info := &domain.VideoInfo{}

	// 使用正则表达式提取关键信息
	durationRegex := regexp.MustCompile(`"duration":\s*"([^"]+)"`)
	bitrateRegex := regexp.MustCompile(`"bit_rate":\s*"([^"]+)"`)
	widthRegex := regexp.MustCompile(`"width":\s*(\d+)`)
	heightRegex := regexp.MustCompile(`"height":\s*(\d+)`)
	codecRegex := regexp.MustCompile(`"codec_name":\s*"([^"]+)"`)
	rFrameRateRegex := regexp.MustCompile(`"r_frame_rate":\s*"([^"]+)"`)
	sizeRegex := regexp.MustCompile(`"size":\s*"([^"]+)"`)

	if matches := durationRegex.FindSubmatch(data); matches != nil {
		info.Duration, _ = strconv.ParseFloat(string(matches[1]), 64)
	}

	if matches := bitrateRegex.FindSubmatch(data); matches != nil {
		br, _ := strconv.ParseInt(string(matches[1]), 10, 64)
		info.Bitrate = br
	}

	if matches := widthRegex.FindSubmatch(data); matches != nil {
		info.Width, _ = strconv.Atoi(string(matches[1]))
	}

	if matches := heightRegex.FindSubmatch(data); matches != nil {
		info.Height, _ = strconv.Atoi(string(matches[1]))
	}

	// 查找第一个视频流的 codec
	codecMatches := codecRegex.FindAllSubmatch(data, -1)
	if len(codecMatches) >= 1 {
		info.Codec = string(codecMatches[0][1])
	}
	if len(codecMatches) >= 2 {
		info.AudioCodec = string(codecMatches[1][1])
	}

	if matches := rFrameRateRegex.FindSubmatch(data); matches != nil {
		fpsParts := strings.Split(string(matches[1]), "/")
		if len(fpsParts) == 2 {
			num, _ := strconv.Atoi(fpsParts[0])
			den, _ := strconv.Atoi(fpsParts[1])
			if den > 0 {
				info.FPS = float64(num) / float64(den)
			}
		}
	}

	if matches := sizeRegex.FindSubmatch(data); matches != nil {
		info.FileSize, _ = strconv.ParseInt(string(matches[1]), 10, 64)
	}

	return info, nil
}

// GenerateThumbnail 生成视频封面
func (t *FFmpegTranscoder) GenerateThumbnail(ctx context.Context, inputPath string, outputPath string, timeOffset float64) error {
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.2f", timeOffset),
		"-i", inputPath,
		"-vframes", "1",
		"-q:v", "2",
		outputPath,
	}

	cmd := exec.CommandContext(ctx, t.ffmpegPath, args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg thumbnail failed: %w", err)
	}

	return nil
}

// Cleanup 清理临时文件
func (t *FFmpegTranscoder) Cleanup(dir string) error {
	return os.RemoveAll(dir)
}
