package ffmpeg

import (
	"context"
	"fmt"
	"os/exec"
)

// FFmpeg 封装 FFmpeg 命令行工具
type FFmpeg struct {
	path string
}

// NewFFmpeg 创建 FFmpeg 实例
func NewFFmpeg() (*FFmpeg, error) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found: %w", err)
	}
	return &FFmpeg{path: path}, nil
}

// Path 返回 FFmpeg 可执行文件路径
func (f *FFmpeg) Path() string {
	return f.path
}

// Command 创建 FFmpeg 命令
func (f *FFmpeg) Command(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, f.path, args...)
}

// Run 执行 FFmpeg 命令
func (f *FFmpeg) Run(ctx context.Context, args ...string) error {
	cmd := f.Command(ctx, args...)
	return cmd.Run()
}
