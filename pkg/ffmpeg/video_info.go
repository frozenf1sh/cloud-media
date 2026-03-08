package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// VideoInfo 视频信息
type VideoInfo struct {
	Duration   float64 // 时长（秒）
	Width      int     // 宽度
	Height     int     // 高度
	Rotation   int     // 旋转角度（0, 90, 180, 270）
	Codec      string  // 视频编码
	Bitrate    int64   // 码率（bps）
	FPS        float64 // 帧率
	AudioCodec string  // 音频编码
	FileSize   int64   // 文件大小（字节）
}

// ffprobeJSON ffprobe 输出的 JSON 结构
type ffprobeJSON struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Rotation    any    `json:"rotation"` // 可能是字符串或数字
		RFrameRate  string `json:"r_frame_rate"`
		SideDataList []struct {
			SideDataType string `json:"side_data_type"`
			Rotation     any    `json:"rotation,omitempty"`
		} `json:"side_data_list,omitempty"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
		Size     string `json:"size"`
	} `json:"format"`
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

	// 先尝试完整解析
	var probe ffprobeJSON
	if err := json.Unmarshal(data, &probe); err == nil {
		// 解析 format
		if probe.Format.Duration != "" {
			info.Duration, _ = strconv.ParseFloat(probe.Format.Duration, 64)
		}
		if probe.Format.BitRate != "" {
			info.Bitrate, _ = strconv.ParseInt(probe.Format.BitRate, 10, 64)
		}
		if probe.Format.Size != "" {
			info.FileSize, _ = strconv.ParseInt(probe.Format.Size, 10, 64)
		}

		// 解析 streams
		for _, stream := range probe.Streams {
			if stream.CodecType == "video" {
				info.Width = stream.Width
				info.Height = stream.Height
				info.Codec = stream.CodecName
				info.FPS = parseFPS(stream.RFrameRate)

				// 尝试从 rotation 字段获取
				info.Rotation = parseRotationValue(stream.Rotation)

				// 如果没找到，尝试从 side_data_list 获取
				if info.Rotation == 0 {
					for _, sideData := range stream.SideDataList {
						if sideData.SideDataType == "Display Matrix" {
							info.Rotation = parseRotationValue(sideData.Rotation)
							if info.Rotation != 0 {
								break
							}
						}
					}
				}
			} else if stream.CodecType == "audio" && info.AudioCodec == "" {
				info.AudioCodec = stream.CodecName
			}
		}

		return info, nil
	}

	// 备用：使用正则表达式解析
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

	// 尝试多种方式解析 rotation
	info.Rotation = extractRotation(data)

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

func parseRotationValue(v any) int {
	switch val := v.(type) {
	case float64:
		return normalizeRotation(int(val))
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return normalizeRotation(i)
		}
	}
	return 0
}

func normalizeRotation(r int) int {
	r = r % 360
	if r < 0 {
		r += 360
	}
	return r
}

func extractRotation(data []byte) int {
	// 尝试多种 rotation 字段模式
	patterns := []string{
		`"rotation":\s*"-?(\d+)"`,
		`"rotation":\s*-?(\d+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindSubmatch(data); matches != nil {
			if r, err := strconv.Atoi(string(matches[1])); err == nil {
				// 检查是否有负号
				fullMatch := string(matches[0])
				if strings.Contains(fullMatch, `"-`) || strings.Contains(fullMatch, `:-`) {
					r = -r
				}
				return normalizeRotation(r)
			}
		}
	}

	// 尝试从 displaymatrix 中提取
	if bytes.Contains(data, []byte(`"side_data_type":"Display Matrix"`)) {
		if bytes.Contains(data, []byte(`"rotation":-90`)) || bytes.Contains(data, []byte(`"rotation":"-90"`)) {
			return 270
		}
		if bytes.Contains(data, []byte(`"rotation":90`)) || bytes.Contains(data, []byte(`"rotation":"90"`)) {
			return 90
		}
		if bytes.Contains(data, []byte(`"rotation":180`)) || bytes.Contains(data, []byte(`"rotation":"180"`)) {
			return 180
		}
		if bytes.Contains(data, []byte(`"rotation":270`)) || bytes.Contains(data, []byte(`"rotation":"270"`)) {
			return 270
		}
	}

	return 0
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
