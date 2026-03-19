package ffmpeg

import "fmt"

// 宽高比常量
const (
	MinAspectRatio = 1.0 / 16.0 // 最小宽高比 1:16
	MaxAspectRatio = 16.0 / 1.0 // 最大宽高比 16:1
)

// ScaleCalculator 视频缩放计算器，用于计算保持宽高比的缩放尺寸
type ScaleCalculator struct{}

// NewScaleCalculator 创建缩放计算器
func NewScaleCalculator() *ScaleCalculator {
	return &ScaleCalculator{}
}

// Calculate 计算保持宽高比的缩放尺寸
//   - 横屏视频（宽>=高）：固定高度，按比例计算宽度
//   - 竖屏视频（高>宽）：固定宽度，按比例计算高度
func (c *ScaleCalculator) Calculate(originalWidth, originalHeight, targetSize int) (width, height int) {
	return c.CalculateWithRotation(originalWidth, originalHeight, targetSize, 0)
}

// CalculateWithRotation 计算保持宽高比的缩放尺寸（支持旋转角度）
// 会根据 rotation 自动调整宽高
func (c *ScaleCalculator) CalculateWithRotation(originalWidth, originalHeight, targetSize, rotation int) (width, height int) {
	if originalWidth <= 0 || originalHeight <= 0 {
		return 1920, 1080
	}

	// 处理 rotation: 90 或 270 度需要交换宽高
	w, h := originalWidth, originalHeight
	if rotation == 90 || rotation == 270 || rotation == -90 || rotation == -270 {
		w, h = h, w
	}

	isPortrait := h > w

	if isPortrait {
		// 竖屏视频：固定宽度，按比例计算高度
		width = targetSize
		height = int(float64(h) * float64(targetSize) / float64(w))
	} else {
		// 横屏视频：固定高度，按比例计算宽度
		height = targetSize
		width = int(float64(w) * float64(targetSize) / float64(h))
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
// 使用 scale=w:h + setsar=1:1 确保不被拉伸变形
func (c *ScaleCalculator) ScaleFilter(width, height int) string {
	// setsar=1:1: 设置样本宽高比为1:1，确保像素是正方形
	return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1:1", width, height)
}

// ensureEven 确保数字是偶数
func ensureEven(n int) int {
	if n%2 != 0 {
		return n - 1
	}
	return n
}
