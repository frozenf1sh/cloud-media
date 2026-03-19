package ffmpeg

import "fmt"

// RotationFilterType 旋转滤镜类型
type RotationFilterType int

const (
	// RotationFilterNone 无旋转
	RotationFilterNone RotationFilterType = iota
	// RotationFilterTranspose1 逆时针旋转90度（transpose=1）
	RotationFilterTranspose1
	// RotationFilterTranspose2 顺时针旋转90度（transpose=2）
	RotationFilterTranspose2
	// RotationFilterTranspose1x2 旋转180度（transpose=1,transpose=1）
	RotationFilterTranspose1x2
)

// GetRotationFilterType 根据 rotation 值获取旋转滤镜类型
// 注意：FFmpeg 的 transpose 滤镜方向与 rotation metadata 相反
func GetRotationFilterType(rotation int) RotationFilterType {
	switch rotation {
	case 90, -270:
		// rotation=90 表示视频本身逆时针转了90度，需要顺时针转回来
		return RotationFilterTranspose2
	case 180, -180:
		// 旋转180度
		return RotationFilterTranspose1x2
	case 270, -90:
		// rotation=270 表示视频本身顺时针转了270度（等于逆时针90度），需要逆时针转回来
		return RotationFilterTranspose1
	default:
		return RotationFilterNone
	}
}

// BuildRotationFilter 构建旋转滤镜字符串
func BuildRotationFilter(rotation int) string {
	filterType := GetRotationFilterType(rotation)
	switch filterType {
	case RotationFilterTranspose2:
		return "transpose=2"
	case RotationFilterTranspose1x2:
		return "transpose=1,transpose=1"
	case RotationFilterTranspose1:
		return "transpose=1"
	default:
		return ""
	}
}

// ApplyRotationToLabel 对输入标签应用旋转滤镜，返回新的标签
func ApplyRotationToLabel(inputLabel string, rotation int) (filter string, outputLabel string) {
	filterType := GetRotationFilterType(rotation)
	switch filterType {
	case RotationFilterTranspose2:
		return fmt.Sprintf("[%s]transpose=2[rotated]", inputLabel), "rotated"
	case RotationFilterTranspose1x2:
		return fmt.Sprintf("[%s]transpose=1,transpose=1[rotated]", inputLabel), "rotated"
	case RotationFilterTranspose1:
		return fmt.Sprintf("[%s]transpose=1[rotated]", inputLabel), "rotated"
	default:
		return "", inputLabel
	}
}

// GetEffectiveDimensions 获取考虑 rotation 后的有效宽高
func GetEffectiveDimensions(width, height, rotation int) (int, int) {
	if rotation == 90 || rotation == 270 || rotation == -90 || rotation == -270 {
		return height, width
	}
	return width, height
}

// IsPortrait 判断视频是否为竖屏（考虑 rotation）
func IsPortrait(width, height, rotation int) bool {
	w, h := GetEffectiveDimensions(width, height, rotation)
	return h > w
}
