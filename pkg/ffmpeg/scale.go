package ffmpeg

import (
	"errors"
	"fmt"
)

// 宽高比限制
const (
	MinAspectRatio = 1.0 / 16.0 // 最小宽高比 1:16
	MaxAspectRatio = 16.0 / 1.0 // 最大宽高比 16:1
)

var ErrInvalidAspectRatio = errors.New("invalid aspect ratio")

// AspectRatioValidator 宽高比验证器
type AspectRatioValidator struct {
	minRatio float64
	maxRatio float64
}

// NewAspectRatioValidator 创建宽高比验证器
func NewAspectRatioValidator(minRatio, maxRatio float64) *AspectRatioValidator {
	return &AspectRatioValidator{
		minRatio: minRatio,
		maxRatio: maxRatio,
	}
}

// NewDefaultAspectRatioValidator 创建默认宽高比验证器
func NewDefaultAspectRatioValidator() *AspectRatioValidator {
	return NewAspectRatioValidator(MinAspectRatio, MaxAspectRatio)
}

// Validate 验证宽高比
func (v *AspectRatioValidator) Validate(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid dimensions: %dx%d", width, height)
	}

	ratio := float64(width) / float64(height)
	if ratio < v.minRatio || ratio > v.maxRatio {
		return fmt.Errorf("%w: %.2f (width:%d, height:%d), must be between %.2f and %.2f",
			ErrInvalidAspectRatio, ratio, width, height, v.minRatio, v.maxRatio)
	}
	return nil
}

// ScaleCalculator 缩放计算器
type ScaleCalculator struct{}

// NewScaleCalculator 创建缩放计算器
func NewScaleCalculator() *ScaleCalculator {
	return &ScaleCalculator{}
}

// Calculate 计算保持宽高比的缩放尺寸
// 横屏视频（宽>=高）：固定高度，按比例计算宽度
// 竖屏视频（高>宽）：固定宽度，按比例计算高度
func (c *ScaleCalculator) Calculate(originalWidth, originalHeight, targetSize int) (width, height int) {
	if originalWidth <= 0 || originalHeight <= 0 {
		return 1920, 1080
	}

	isPortrait := originalHeight > originalWidth

	if isPortrait {
		// 竖屏视频：固定宽度，按比例计算高度
		width = targetSize
		height = int(float64(originalHeight) * float64(targetSize) / float64(originalWidth))
	} else {
		// 横屏视频：固定高度，按比例计算宽度
		height = targetSize
		width = int(float64(originalWidth) * float64(targetSize) / float64(originalHeight))
	}

	// 确保宽度和高度是偶数（H.264 编码要求）
	width = ensureEven(width)
	height = ensureEven(height)

	// 确保不小于 2
	if width < 2 {
		width = 2
	}
	if height < 2 {
		height = 2
	}

	return width, height
}

// ScaleFilter 生成 FFmpeg scale filter 字符串
func (c *ScaleCalculator) ScaleFilter(width, height int) string {
	return fmt.Sprintf("scale=%d:%d:flags=lanczos", width, height)
}

func ensureEven(n int) int {
	if n%2 != 0 {
		return n - 1
	}
	return n
}
