package ffmpeg

import (
	"context"
	"fmt"
	"os"
)

// ValidationConfig 验证配置
type ValidationConfig struct {
	MinFileSize int64 // 最小文件大小（字节），默认 100 字节
	MaxFileSize int64 // 最大文件大小（字节），默认 10GB
}

// ValidationResult 验证结果，包含验证通过的视频信息
type ValidationResult struct {
	Valid     bool
	VideoInfo *VideoInfo // 验证通过时返回视频信息
}

// DefaultValidationConfig 默认验证配置
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		MinFileSize: 100,
		MaxFileSize: 10 * 1024 * 1024 * 1024, // 10GB
	}
}

// VideoValidator 视频文件验证器
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

	return videoInfo, nil
}
