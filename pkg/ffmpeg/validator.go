package ffmpeg

import (
	"context"
	"fmt"
	"os"
)

// ValidationConfig 视频验证配置
type ValidationConfig struct {
	MinFileSize    int64   // 最小文件大小（字节），默认 100 字节
	MaxFileSize    int64   // 最大文件大小（字节），默认 10GB
	MinDuration    float64 // 最小视频时长（秒），默认 0（不限制）
	MaxDuration    float64 // 最大视频时长（秒），默认 86400（24小时）
	MinAspectRatio float64 // 最小宽高比，默认 1/16
	MaxAspectRatio float64 // 最大宽高比，默认 16/1
}

// DefaultValidationConfig 返回默认验证配置
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		MinFileSize:    100,
		MaxFileSize:    10 * 1024 * 1024 * 1024, // 10GB
		MinDuration:    0,                                // 不限制最小时长
		MaxDuration:    24 * 60 * 60,                    // 24小时
		MinAspectRatio: 1.0 / 16.0,                       // 1:16
		MaxAspectRatio: 16.0 / 1.0,                       // 16:1
	}
}

// VideoValidator 视频文件验证器，验证文件是否为有效的视频文件
type VideoValidator struct {
	config     ValidationConfig
	ffprobe    *FFprobe
	infoParser *VideoInfoParser
}

// NewVideoValidator 创建视频验证器
func NewVideoValidator(config ValidationConfig) (*VideoValidator, error) {
	fp, err := NewFFprobe()
	if err != nil {
		return nil, err
	}
	return &VideoValidator{
		config:     config,
		ffprobe:    fp,
		infoParser: NewVideoInfoParser(fp),
	}, nil
}

// NewDefaultVideoValidator 使用默认配置创建视频验证器
func NewDefaultVideoValidator() (*VideoValidator, error) {
	return NewVideoValidator(DefaultValidationConfig())
}

// ValidateAndGetInfo 验证视频文件并返回视频信息（避免重复 FFprobe 调用）
// 验证步骤：
//  1. 检查文件是否存在
//  2. 检查文件大小
//  3. 使用 FFprobe 解析视频流
//  4. 验证视频时长
//  5. 验证宽高比
func (v *VideoValidator) ValidateAndGetInfo(ctx context.Context, filePath string) (*VideoInfo, error) {
	// 1. 检查文件是否存在并获取信息
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %w", err)
		}
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// 2. 检查是否是目录
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file")
	}

	// 3. 检查文件大小（快速过滤无效文件）
	if info.Size() < v.config.MinFileSize {
		return nil, fmt.Errorf("file too small (%d bytes, min %d bytes)", info.Size(), v.config.MinFileSize)
	}
	if info.Size() > v.config.MaxFileSize {
		return nil, fmt.Errorf("file too large (%d bytes, max %d bytes)", info.Size(), v.config.MaxFileSize)
	}

	// 4. 用 FFprobe 验证（这是最可靠的验证）
	videoInfo, err := v.infoParser.Parse(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("not a valid video file: %w", err)
	}

	// 5. 确保有有效的视频流
	if videoInfo.Width == 0 || videoInfo.Height == 0 {
		return nil, fmt.Errorf("video has no valid video stream")
	}

	// 6. 验证视频时长
	if v.config.MinDuration > 0 && videoInfo.Duration < v.config.MinDuration {
		return nil, fmt.Errorf("video too short (%.2fs, min %.2fs)", videoInfo.Duration, v.config.MinDuration)
	}
	if v.config.MaxDuration > 0 && videoInfo.Duration > v.config.MaxDuration {
		return nil, fmt.Errorf("video too long (%.2fs, max %.2fs)", videoInfo.Duration, v.config.MaxDuration)
	}

	// 7. 验证宽高比
	aspectRatio := float64(videoInfo.Width) / float64(videoInfo.Height)
	if aspectRatio < v.config.MinAspectRatio {
		return nil, fmt.Errorf("video aspect ratio too small (%.2f, min %.2f)", aspectRatio, v.config.MinAspectRatio)
	}
	if aspectRatio > v.config.MaxAspectRatio {
		return nil, fmt.Errorf("video aspect ratio too large (%.2f, max %.2f)", aspectRatio, v.config.MaxAspectRatio)
	}

	return videoInfo, nil
}
