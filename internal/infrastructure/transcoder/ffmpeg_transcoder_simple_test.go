package transcoder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frozenf1sh/cloud-media/internal/domain"
)

// TestFFmpegTranscoder_parseResolution 测试解析分辨率
func TestFFmpegTranscoder_parseResolution(t *testing.T) {
	tests := []struct {
		name       string
		res        string
		wantWidth  int
		wantHeight int
	}{
		{
			name:       "1080p",
			res:        "1920x1080",
			wantWidth:  1920,
			wantHeight: 1080,
		},
		{
			name:       "720p",
			res:        "1280x720",
			wantWidth:  1280,
			wantHeight: 720,
		},
		{
			name:       "480p",
			res:        "854x480",
			wantWidth:  854,
			wantHeight: 480,
		},
		{
			name:       "invalid",
			res:        "invalid",
			wantWidth:  1920,
			wantHeight: 1080,
		},
	}

	transcoder := &FFmpegTranscoder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, height := transcoder.parseResolution(tt.res)
			if width != tt.wantWidth {
				t.Errorf("parseResolution(%q) width = %d, want %d", tt.res, width, tt.wantWidth)
			}
			if height != tt.wantHeight {
				t.Errorf("parseResolution(%q) height = %d, want %d", tt.res, height, tt.wantHeight)
			}
		})
	}
}

// TestFFmpegTranscoder_Cleanup 测试清理临时文件
func TestFFmpegTranscoder_Cleanup(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "transcoder-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// 创建一些测试文件
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	transcoder := &FFmpegTranscoder{}
	if err := transcoder.Cleanup(tempDir); err != nil {
		t.Errorf("Cleanup failed: %v", err)
	}

	// 验证目录已被删除
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Error("temp dir should have been deleted")
	}
}

// TestFFmpegTranscoder_generateMasterPlaylist 测试生成主播放列表
func TestFFmpegTranscoder_generateMasterPlaylist(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "playlist-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	playlistPath := filepath.Join(tempDir, "master.m3u8")

	variants := []domain.VariantOutput{
		{
			Resolution:   "1920x1080",
			PlaylistPath: "1920x1080/index.m3u8",
			Bandwidth:    4000000,
		},
		{
			Resolution:   "1280x720",
			PlaylistPath: "1280x720/index.m3u8",
			Bandwidth:    2000000,
		},
	}

	transcoder := &FFmpegTranscoder{}
	err = transcoder.generateMasterPlaylist(playlistPath, variants)
	if err != nil {
		t.Fatalf("generateMasterPlaylist failed: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(playlistPath); os.IsNotExist(err) {
		t.Error("playlist file should exist")
	}

	// 验证内容
	content, err := os.ReadFile(playlistPath)
	if err != nil {
		t.Fatalf("failed to read playlist: %v", err)
	}

	playlistStr := string(content)
	if !strings.Contains(playlistStr, "#EXTM3U") {
		t.Error("playlist should contain #EXTM3U")
	}
	if !strings.Contains(playlistStr, "1920x1080/index.m3u8") {
		t.Error("playlist should contain 1080p variant")
	}
	if !strings.Contains(playlistStr, "1280x720/index.m3u8") {
		t.Error("playlist should contain 720p variant")
	}
}
