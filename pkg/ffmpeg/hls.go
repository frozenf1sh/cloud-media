package ffmpeg

import (
	"fmt"
	"os"
	"strings"
)

// VariantConfig HLS 变体配置
type VariantConfig struct {
	Name           string
	Width          int
	Height         int
	Bitrate        string
	Bandwidth      int
	PlaylistPath   string
	SegmentPattern string
}

// BuildMasterPlaylist 构建 HLS 主播放列表内容
func BuildMasterPlaylist(variants []VariantConfig) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")

	for _, v := range variants {
		sb.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n",
			v.Bandwidth, v.Width, v.Height))
		sb.WriteString(fmt.Sprintf("%s/index.m3u8\n", v.Name))
	}

	return sb.String()
}

// WriteMasterPlaylist 写入 HLS 主播放列表到文件
func WriteMasterPlaylist(path string, variants []VariantConfig) error {
	content := BuildMasterPlaylist(variants)
	return os.WriteFile(path, []byte(content), 0644)
}
