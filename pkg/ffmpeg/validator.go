package ffmpeg

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// 视频文件魔数（文件头标识）
var videoMagicNumbers = map[string][]string{
	"mp4":  {"\x00\x00\x00\x18ftyp", "\x00\x00\x00\x20ftyp", "\x00\x00\x00\x1cftyp"},
	"webm": {"\x1a\x45\xdf\xa3"},
	"mkv":  {"\x1a\x45\xdf\xa3"},
	"avi":  {"RIFF"},
	"mov":  {"\x00\x00\x00\x14ftyp", "\x00\x00\x00\x1cftyp"},
	"flv":  {"FLV\x01"},
	"wmv":  {"\x30\x26\xb2\x75\x8e\x66\xcf\x11"},
	"mts":  {"G"}, // MPEG-TS 通常以同步字节 0x47 开头
}

// 常见的视频文件扩展名
var videoExtensions = map[string]bool{
	".mp4":  true,
	".webm": true,
	".mkv":  true,
	".avi":  true,
	".mov":  true,
	".flv":  true,
	".wmv":  true,
	".mts":  true,
	".m2ts": true,
	".ts":   true,
}

// ValidationConfig 验证配置
type ValidationConfig struct {
	MinFileSize int64 // 最小文件大小（字节），默认 100 字节
	MaxFileSize int64 // 最大文件大小（字节），默认 10GB
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
	config   ValidationConfig
	ffprobe  *FFprobe
	infoParser *VideoInfoParser
}

// NewVideoValidator 创建视频验证器
func NewVideoValidator(config ValidationConfig) (*VideoValidator, error) {
	fp, err := NewFFprobe()
	if err != nil {
		return nil, err
	}
	return &VideoValidator{
		config:   config,
		ffprobe:  fp,
		infoParser: NewVideoInfoParser(fp),
	}, nil
}

// NewDefaultVideoValidator 使用默认配置创建视频验证器
func NewDefaultVideoValidator() (*VideoValidator, error) {
	return NewVideoValidator(DefaultValidationConfig())
}

// Validate 验证视频文件
func (v *VideoValidator) Validate(ctx context.Context, filePath string) error {
	// 1. 检查文件是否存在
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %w", err)
		}
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// 2. 检查是否是目录
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file")
	}

	// 3. 检查文件大小
	if info.Size() < v.config.MinFileSize {
		return fmt.Errorf("file too small (%d bytes, min %d bytes)", info.Size(), v.config.MinFileSize)
	}
	if info.Size() > v.config.MaxFileSize {
		return fmt.Errorf("file too large (%d bytes, max %d bytes)", info.Size(), v.config.MaxFileSize)
	}

	// 4. 检查文件魔数
	if err := v.validateMagicNumber(filePath); err != nil {
		return err
	}

	// 5. 尝试用 FFprobe 解析，验证是否是有效的视频文件
	if err := v.validateWithFFprobe(ctx, filePath); err != nil {
		return err
	}

	return nil
}

// validateMagicNumber 通过魔数验证文件类型
func (v *VideoValidator) validateMagicNumber(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 读取文件头（至少 16 字节）
	header := make([]byte, 32)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read file header: %w", err)
	}
	if n < 4 {
		return fmt.Errorf("file too short to be a valid video")
	}

	// 检查扩展名
	ext := filepath.Ext(filePath)
	if ext != "" {
		if !videoExtensions[ext] {
			// 扩展名不是视频类型，但继续检查魔数
		}
	}

	// 检查魔数
	isValid := false
	for _, magics := range videoMagicNumbers {
		for _, magic := range magics {
			if len(magic) <= n && string(header[:len(magic)]) == magic {
				isValid = true
				break
			}
		}
		if isValid {
			break
		}
	}

	// 魔数检查是启发式的，即使不匹配也不直接拒绝
	// 而是继续用 FFprobe 验证

	return nil
}

// validateWithFFprobe 使用 FFprobe 验证视频
func (v *VideoValidator) validateWithFFprobe(ctx context.Context, filePath string) error {
	info, err := v.infoParser.Parse(ctx, filePath)
	if err != nil {
		return fmt.Errorf("not a valid video file: %w", err)
	}

	// 检查是否有视频流
	if info.Width == 0 || info.Height == 0 {
		return fmt.Errorf("video has no valid video stream")
	}

	return nil
}

// IsVideoFile 快速检查文件是否可能是视频文件（基于魔数）
func IsVideoFile(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	header := make([]byte, 32)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return false
	}
	if n < 4 {
		return false
	}

	for _, magics := range videoMagicNumbers {
		for _, magic := range magics {
			if len(magic) <= n && string(header[:len(magic)]) == magic {
				return true
			}
		}
	}

	return false
}
