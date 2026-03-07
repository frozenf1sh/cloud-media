package domain

import (
	"context"
	"io"
)

// VideoTaskStatus 任务状态类型
type VideoTaskStatus string

const (
	TaskStatusPending    VideoTaskStatus = "pending"
	TaskStatusQueued     VideoTaskStatus = "queued"
	TaskStatusProcessing VideoTaskStatus = "processing"
	TaskStatusSuccess    VideoTaskStatus = "success"
	TaskStatusFailed     VideoTaskStatus = "failed"
	TaskStatusCancelled  VideoTaskStatus = "cancelled"
)

// VideoTask 视频转码任务领域模型
type VideoTask struct {
	ID              uint
	TaskID          string
	TraceID         string          // 全链路追踪 ID
	SourceKey       string
	SourceBucket    string
	SourceSize      int64           // 源文件大小（字节）
	SourceDuration  float64         // 源视频时长（秒）
	OutputInfo      *OutputInfo     // 输出信息（支持 HLS 多文件）
	Status          VideoTaskStatus
	Progress        int
	TranscodeConfig *TranscodeConfig
	ErrorMessage    string
	CreatedAt       int64
	UpdatedAt       int64
	StartedAt       *int64
	CompletedAt     *int64
}

// OutputInfo 输出信息（支持 HLS 多文件输出）
type OutputInfo struct {
	OutputBasePath  string          `json:"output_base_path"`  // 输出基路径
	OutputBucket    string          `json:"output_bucket"`      // 输出存储桶
	PlaylistPath    string          `json:"playlist_path"`      // 主播放列表路径（HLS master.m3u8）
	ThumbnailPath   string          `json:"thumbnail_path"`     // 封面图路径
	Variants        []VariantOutput `json:"variants"`           // 多码率变体
	PreviewDuration float64         `json:"preview_duration"`   // 已可预览的时长（秒）
}

// VariantOutput HLS 多码率变体输出
type VariantOutput struct {
	Resolution   string `json:"resolution"`    // 分辨率，如 "1920x1080"
	PlaylistPath string `json:"playlist_path"` // 该变体的播放列表路径
	Bandwidth    int    `json:"bandwidth"`     // 码率（bps）
}

// TranscodeConfig 转码配置
type TranscodeConfig struct {
	OutputFormat string                 `json:"output_format"`
	Video        VideoTranscodeConfig   `json:"video"`
	Audio        AudioTranscodeConfig   `json:"audio"`
	Watermark    *WatermarkConfig       `json:"watermark,omitempty"`
}

// VideoTranscodeConfig 视频转码配置
type VideoTranscodeConfig struct {
	Codec      string `json:"codec"`
	Bitrate    string `json:"bitrate"`
	Resolution string `json:"resolution"`
	FPS        int    `json:"fps"`
}

// AudioTranscodeConfig 音频转码配置
type AudioTranscodeConfig struct {
	Codec       string `json:"codec"`
	Bitrate     string `json:"bitrate"`
	SampleRate  int    `json:"sample_rate"`
}

// WatermarkConfig 水印配置
type WatermarkConfig struct {
	Enabled   bool   `json:"enabled"`
	ImageKey  string `json:"image_key"`
	Position  string `json:"position"` // top-left, top-right, bottom-left, bottom-right
}

// TaskStatusLog 任务状态变更日志
type TaskStatusLog struct {
	ID         uint
	TaskID     string
	FromStatus VideoTaskStatus
	ToStatus   VideoTaskStatus
	Message    string
	CreatedAt  int64
}

// VideoTaskRepository 任务仓储接口
type VideoTaskRepository interface {
	Create(ctx context.Context, task *VideoTask) error
	Update(ctx context.Context, task *VideoTask) error
	GetByTaskID(ctx context.Context, taskID string) (*VideoTask, error)
	List(ctx context.Context, page, pageSize int) ([]*VideoTask, int64, error)
	UpdateStatus(ctx context.Context, taskID string, status VideoTaskStatus, message ...string) error
	UpdateProgress(ctx context.Context, taskID string, progress int) error
}

// MQBroker 消息队列接口
type MQBroker interface {
	PublishVideoTask(ctx context.Context, task *VideoTask) error
}

// ObjectStorage 对象存储接口 - 支持 MinIO、S3、阿里云 OSS 等
type ObjectStorage interface {
	// UploadFile 上传文件到指定存储桶
	UploadFile(ctx context.Context, bucket, key string, data []byte) error
	// DownloadFile 下载文件
	DownloadFile(ctx context.Context, bucket, key string) ([]byte, error)
	// GetPresignedURL 获取预签名 URL（用于分片上传/下载）
	GetPresignedURL(ctx context.Context, bucket, key string, method string, expiry int64) (string, error)
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
	Duration  float64 // 时长（秒）
	Width     int     // 宽度
	Height    int     // 高度
	Codec     string  // 视频编码
	Bitrate   int64   // 码率（bps）
	FPS       float64 // 帧率
	AudioCodec string // 音频编码
	FileSize  int64   // 文件大小（字节）
}
