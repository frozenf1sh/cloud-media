package domain

import (
	"context"
	"io"
)

// ReliableMQBroker 可靠消息队列接口（支持发布确认）
type ReliableMQBroker interface {
	// PublishWithConfirm 带确认的发布
	PublishWithConfirm(ctx context.Context, event *OutboxEvent) error
	// Start 启动连接和确认监听
	Start(ctx context.Context) error
	// Stop 停止
	Stop() error
}

// ObjectStorage 对象存储接口 - 支持 MinIO、S3、阿里云 OSS 等
type ObjectStorage interface {
	// UploadFile 上传文件到指定存储桶
	UploadFile(ctx context.Context, bucket, key string, data []byte) error
	// DownloadFile 下载文件
	DownloadFile(ctx context.Context, bucket, key string) ([]byte, error)
	// GetPresignedURL 获取预签名 URL（用于分片上传/下载）
	GetPresignedURL(ctx context.Context, bucket, key string, method string, expiry int64) (string, error)
	// GetPublicURL 获取公开访问 URL（桶已公开读时使用）
	GetPublicURL(ctx context.Context, bucket, key string) (string, error)
	// ListObjects 列出存储桶中的对象
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
	// DeleteObject 删除对象
	DeleteObject(ctx context.Context, bucket, key string) error
	// UploadFromReader 从 io.Reader 上传文件
	UploadFromReader(ctx context.Context, bucket, key string, reader io.Reader, size int64) error
	// DownloadToWriter 下载文件到 io.Writer
	DownloadToWriter(ctx context.Context, bucket, key string, writer io.Writer) error
	// EnsureBucketExists 确保存储桶存在，不存在则创建
	EnsureBucketExists(ctx context.Context, bucket string) error
}

// TranscodeProgressCallback 转码进度回调函数
type TranscodeProgressCallback func(progress int, message string)

// Transcoder 视频转码器接口
type Transcoder interface {
	// Transcode 执行视频转码
	// inputPath: 输入视频文件路径
	// outputDir: 输出目录
	// config: 转码配置
	// videoInfo: 可选的预获取视频信息（如果为 nil 会自动获取）
	// onProgress: 进度回调
	// 返回: 输出信息，错误
	Transcode(
		ctx context.Context,
		inputPath string,
		outputDir string,
		config *TranscodeConfig,
		videoInfo *VideoInfo,
		onProgress TranscodeProgressCallback,
	) (*OutputInfo, error)

	// GetVideoInfo 获取并验证视频信息
	GetVideoInfo(ctx context.Context, inputPath string) (*VideoInfo, error)

	// GenerateThumbnail 生成视频封面
	GenerateThumbnail(ctx context.Context, inputPath string, outputPath string, timeOffset float64, videoInfo *VideoInfo) error
}

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
