package ffmpeg

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

// VideoInfo 视频信息
type VideoInfo struct {
	Duration   float64 // 时长（秒）
	Width      int     // 宽度
	Height     int     // 高度
	Codec      string  // 视频编码
	Bitrate    int64   // 码率（bps）
	FPS        float64 // 帧率
	AudioCodec string  // 音频编码
	FileSize   int64   // 文件大小（字节）
}

// VideoInfoParser 视频信息解析器
type VideoInfoParser struct {
	ffprobe *FFprobe
}

// NewVideoInfoParser 创建视频信息解析器
func NewVideoInfoParser(ffprobe *FFprobe) *VideoInfoParser {
	return &VideoInfoParser{ffprobe: ffprobe}
}

// Parse 解析视频文件信息
func (p *VideoInfoParser) Parse(ctx context.Context, inputPath string) (*VideoInfo, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	}

	output, err := p.ffprobe.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	return p.parseJSONOutput(output)
}

func (p *VideoInfoParser) parseJSONOutput(data []byte) (*VideoInfo, error) {
	info := &VideoInfo{}

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
		fpsParts := string(matches[1])
		info.FPS = parseFPS(fpsParts)
	}

	if matches := sizeRegex.FindSubmatch(data); matches != nil {
		info.FileSize, _ = strconv.ParseInt(string(matches[1]), 10, 64)
	}

	return info, nil
}

func parseFPS(fpsStr string) float64 {
	parts := splitTwo(fpsStr, "/")
	if len(parts) != 2 {
		return 0
	}
	num, err1 := strconv.Atoi(parts[0])
	den, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || den <= 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func splitTwo(s, sep string) []string {
	idx := firstIndex(s, sep)
	if idx < 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+len(sep):]}
}

func firstIndex(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
