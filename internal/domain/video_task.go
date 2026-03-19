// Package domain 定义领域模型和核心业务接口。
//
// 包含：
//   - VideoTask - 视频转码任务模型
//   - OutputInfo - HLS 输出信息
//   - TranscodeConfig - 转码配置
package domain

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
	Codec      string `json:"codec"`
	Bitrate    string `json:"bitrate"`
	SampleRate int    `json:"sample_rate"`
}

// WatermarkConfig 水印配置
type WatermarkConfig struct {
	Enabled  bool   `json:"enabled"`
	ImageKey string `json:"image_key"`
	Position string `json:"position"` // top-left, top-right, bottom-left, bottom-right
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
