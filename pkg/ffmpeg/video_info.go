// Package ffmpeg 提供 FFmpeg 和 FFprobe 的 Go 封装，用于视频处理和信息提取。
//
// 主要功能：
//   - 视频信息解析（使用 FFprobe）
//   - 视频文件验证
//   - 视频转码进度解析
//   - 视频缩放计算
package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// VideoInfo 视频文件的元数据信息
type VideoInfo struct {
	Duration   float64 // 视频时长（秒）
	Width      int     // 视频宽度（像素）
	Height     int     // 视频高度（像素）
	Rotation   int     // 视频旋转角度（0, 90, 180, 270）
	Codec      string  // 视频编码格式（如 h264, hevc）
	Bitrate    int64   // 视频码率（bps）
	FPS        float64 // 帧率（每秒帧数）
	AudioCodec string  // 音频编码格式
	FileSize   int64   // 文件大小（字节）
}

// ffprobeJSON ffprobe JSON 输出结构，仅用于内部解析
type ffprobeJSON struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		Rotation     any    `json:"rotation"` // 可能是字符串或数字
		RFrameRate   string `json:"r_frame_rate"`
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

// VideoInfoParser 视频信息解析器，封装 FFprobe 调用
type VideoInfoParser struct {
	ffprobe *FFprobe
}

// NewVideoInfoParser 创建视频信息解析器
func NewVideoInfoParser(ffprobe *FFprobe) *VideoInfoParser {
	return &VideoInfoParser{ffprobe: ffprobe}
}

// Parse 解析视频文件并返回视频信息
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

// parseJSONOutput 解析 ffprobe 的 JSON 输出
func (p *VideoInfoParser) parseJSONOutput(data []byte) (*VideoInfo, error) {
	info := &VideoInfo{}

	var probe ffprobeJSON
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	// 解析 format 信息
	if probe.Format.Duration != "" {
		info.Duration, _ = strconv.ParseFloat(probe.Format.Duration, 64)
	}
	if probe.Format.BitRate != "" {
		info.Bitrate, _ = strconv.ParseInt(probe.Format.BitRate, 10, 64)
	}
	if probe.Format.Size != "" {
		info.FileSize, _ = strconv.ParseInt(probe.Format.Size, 10, 64)
	}

	// 解析 streams 信息
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

// parseRotationValue 解析 rotation 字段值（可能是字符串或数字）
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

// normalizeRotation 将旋转角度标准化为 0-360 范围内
func normalizeRotation(r int) int {
	r = r % 360
	if r < 0 {
		r += 360
	}
	return r
}

// parseFPS 解析帧率字符串（如 "30/1"）
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

// splitTwo 按分隔符分割字符串为两部分
func splitTwo(s, sep string) []string {
	idx := firstIndex(s, sep)
	if idx < 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+len(sep):]}
}

// firstIndex 查找分隔符在字符串中的首次出现位置
func firstIndex(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
