package ffmpeg

import (
	"context"
	"fmt"
	"os/exec"
)

// FFprobe 封装 FFprobe 命令行工具
type FFprobe struct {
	path string
}

// NewFFprobe 创建 FFprobe 实例，自动查找系统中的 ffprobe 可执行文件
func NewFFprobe() (*FFprobe, error) {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %w", err)
	}
	return &FFprobe{path: path}, nil
}

// Path 返回 FFprobe 可执行文件路径
func (f *FFprobe) Path() string {
	return f.path
}

// Command 创建 FFprobe 命令，返回 *exec.Cmd
func (f *FFprobe) Command(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, f.path, args...)
}

// Run 执行 FFprobe 命令并返回输出
func (f *FFprobe) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := f.Command(ctx, args...)
	return cmd.Output()
}
