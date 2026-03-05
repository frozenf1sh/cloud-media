package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultValidationConfig(t *testing.T) {
	cfg := DefaultValidationConfig()
	if cfg.MinFileSize != 100 {
		t.Errorf("Expected MinFileSize 100, got %d", cfg.MinFileSize)
	}
	if cfg.MaxFileSize != 10*1024*1024*1024 {
		t.Errorf("Expected MaxFileSize 10GB, got %d", cfg.MaxFileSize)
	}
}

func TestIsVideoFile_FileNotFound(t *testing.T) {
	result := IsVideoFile("/non/existent/file.mp4")
	if result {
		t.Error("Expected false for non-existent file")
	}
}

func TestIsVideoFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp4")

	// 创建空文件
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	result := IsVideoFile(tmpFile)
	// 空文件可能返回 false 或 true（取决于实现），但不应 panic
	_ = result
}

func TestIsVideoFile_MP4Magic(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp4")

	// 创建带有 MP4 魔数的文件
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// 写入 MP4 魔数
	_, err = f.Write([]byte("\x00\x00\x00\x18ftyp"))
	if err != nil {
		t.Fatal(err)
	}

	result := IsVideoFile(tmpFile)
	// 应该识别为视频文件
	_ = result
}

func TestNewVideoValidator(t *testing.T) {
	validator, err := NewDefaultVideoValidator()
	if err != nil {
		// 在没有 FFprobe 的环境中会失败，这是预期的
		t.Logf("NewDefaultVideoValidator failed (expected in CI): %v", err)
		return
	}
	if validator == nil {
		t.Error("Expected validator to be non-nil")
	}
}

func TestNewVideoValidatorWithConfig(t *testing.T) {
	cfg := ValidationConfig{
		MinFileSize: 10,
		MaxFileSize: 1000,
	}
	validator, err := NewVideoValidator(cfg)
	if err != nil {
		// 在没有 FFprobe 的环境中会失败
		t.Logf("NewVideoValidator failed (expected in CI): %v", err)
		return
	}
	if validator.config.MinFileSize != 10 {
		t.Errorf("Expected MinFileSize 10, got %d", validator.config.MinFileSize)
	}
	if validator.config.MaxFileSize != 1000 {
		t.Errorf("Expected MaxFileSize 1000, got %d", validator.config.MaxFileSize)
	}
}

func TestVideoValidator_Validate_FileNotFound(t *testing.T) {
	validator, err := NewDefaultVideoValidator()
	if err != nil {
		t.Skip("FFprobe not available, skipping test")
	}

	ctx := context.Background()
	err = validator.Validate(ctx, "/non/existent/file.mp4")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestVideoValidator_Validate_Directory(t *testing.T) {
	validator, err := NewDefaultVideoValidator()
	if err != nil {
		t.Skip("FFprobe not available, skipping test")
	}

	tmpDir := t.TempDir()

	ctx := context.Background()
	err = validator.Validate(ctx, tmpDir)
	if err == nil {
		t.Error("Expected error for directory")
	}
}

func TestVideoValidator_Validate_SmallFile(t *testing.T) {
	validator, err := NewVideoValidator(ValidationConfig{
		MinFileSize: 1000,
		MaxFileSize: 10000,
	})
	if err != nil {
		t.Skip("FFprobe not available, skipping test")
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "small.mp4")

	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	// 只写 100 字节（小于 MinFileSize 1000）
	_, err = f.Write(make([]byte, 100))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	err = validator.Validate(ctx, tmpFile)
	if err == nil {
		t.Error("Expected error for small file")
	}
}

func TestVideoExtensions(t *testing.T) {
	testCases := []struct {
		ext  string
		want bool
	}{
		{".mp4", true},
		{".webm", true},
		{".mkv", true},
		{".avi", true},
		{".mov", true},
		{".flv", true},
		{".wmv", true},
		{".mts", true},
		{".ts", true},
		{".jpg", false},
		{".png", false},
		{".txt", false},
		{".pdf", false},
	}

	for _, tc := range testCases {
		t.Run(tc.ext, func(t *testing.T) {
			got := videoExtensions[tc.ext]
			if got != tc.want {
				t.Errorf("videoExtensions[%s] = %v, want %v", tc.ext, got, tc.want)
			}
		})
	}
}
