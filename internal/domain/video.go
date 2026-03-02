package domain

import "context"

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
	SourceKey       string
	SourceBucket    string
	TargetKey       string
	TargetBucket    string
	Status          VideoTaskStatus
	Progress        int
	TranscodeConfig *TranscodeConfig
	ErrorMessage    string
	CreatedAt       int64
	UpdatedAt       int64
	StartedAt       *int64
	CompletedAt     *int64
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
	PublishVideoTask(task *VideoTask) error
}
