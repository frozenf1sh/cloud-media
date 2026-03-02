package persistence

import (
	"encoding/json"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// VideoTaskModel GORM 模型 - 对应 video_tasks 表
type VideoTaskModel struct {
	ID               uint           `gorm:"primaryKey;autoIncrement"`
	TaskID           string         `gorm:"size:64;uniqueIndex;not null"`
	SourceKey        string         `gorm:"size:512;not null"`
	SourceBucket     string         `gorm:"size:64;not null"`
	SourceSize       int64          // 源文件大小（字节）
	SourceDuration   float64        // 源视频时长（秒）
	OutputInfo       datatypes.JSON `gorm:"type:jsonb"` // 输出信息（支持 HLS）
	Status           string         `gorm:"size:32;index;not null"`
	Progress         uint8          `gorm:"default:0"`
	TranscodeConfig  datatypes.JSON `gorm:"type:jsonb"`
	ErrorMessage     string         `gorm:"type:text"`
	StartedAt        *time.Time
	CompletedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// 关联
	StatusLogs []TaskStatusLogModel `gorm:"foreignKey:TaskID;references:TaskID"`
}

// TableName 指定表名
func (VideoTaskModel) TableName() string {
	return "video_tasks"
}

// ToDomain 转换为领域模型
func (m *VideoTaskModel) ToDomain() *domain.VideoTask {
	task := &domain.VideoTask{
		ID:             m.ID,
		TaskID:         m.TaskID,
		SourceKey:      m.SourceKey,
		SourceBucket:   m.SourceBucket,
		SourceSize:     m.SourceSize,
		SourceDuration: m.SourceDuration,
		Status:         domain.VideoTaskStatus(m.Status),
		Progress:       int(m.Progress),
		ErrorMessage:   m.ErrorMessage,
		CreatedAt:      m.CreatedAt.Unix(),
		UpdatedAt:      m.UpdatedAt.Unix(),
	}

	if m.StartedAt != nil {
		ts := m.StartedAt.Unix()
		task.StartedAt = &ts
	}
	if m.CompletedAt != nil {
		ts := m.CompletedAt.Unix()
		task.CompletedAt = &ts
	}

	// 反序列化 TranscodeConfig
	if len(m.TranscodeConfig) > 0 {
		var config domain.TranscodeConfig
		if err := json.Unmarshal(m.TranscodeConfig, &config); err == nil {
			task.TranscodeConfig = &config
		}
	}

	// 反序列化 OutputInfo
	if len(m.OutputInfo) > 0 {
		var outputInfo domain.OutputInfo
		if err := json.Unmarshal(m.OutputInfo, &outputInfo); err == nil {
			task.OutputInfo = &outputInfo
		}
	}

	return task
}

// FromDomain 从领域模型创建 GORM 模型
func FromDomain(task *domain.VideoTask) *VideoTaskModel {
	model := &VideoTaskModel{
		ID:             task.ID,
		TaskID:         task.TaskID,
		SourceKey:      task.SourceKey,
		SourceBucket:   task.SourceBucket,
		SourceSize:     task.SourceSize,
		SourceDuration: task.SourceDuration,
		Status:         string(task.Status),
		Progress:       uint8(task.Progress),
		ErrorMessage:   task.ErrorMessage,
	}

	if task.StartedAt != nil {
		t := time.Unix(*task.StartedAt, 0)
		model.StartedAt = &t
	}
	if task.CompletedAt != nil {
		t := time.Unix(*task.CompletedAt, 0)
		model.CompletedAt = &t
	}

	// 序列化 TranscodeConfig
	if task.TranscodeConfig != nil {
		if data, err := json.Marshal(task.TranscodeConfig); err == nil {
			model.TranscodeConfig = datatypes.JSON(data)
		}
	}

	// 序列化 OutputInfo
	if task.OutputInfo != nil {
		if data, err := json.Marshal(task.OutputInfo); err == nil {
			model.OutputInfo = datatypes.JSON(data)
		}
	}

	return model
}

// TaskStatusLogModel GORM 模型 - 对应 task_status_logs 表
type TaskStatusLogModel struct {
	ID         uint      `gorm:"primaryKey;autoIncrement"`
	TaskID     string    `gorm:"size:64;index;not null"`
	FromStatus string    `gorm:"size:32"`
	ToStatus   string    `gorm:"size:32;not null"`
	Message    string    `gorm:"type:text"`
	CreatedAt  time.Time `gorm:"index"`
}

// TableName 指定表名
func (TaskStatusLogModel) TableName() string {
	return "task_status_logs"
}

// ToDomain 转换为领域模型
func (m *TaskStatusLogModel) ToDomain() *domain.TaskStatusLog {
	return &domain.TaskStatusLog{
		ID:         m.ID,
		TaskID:     m.TaskID,
		FromStatus: domain.VideoTaskStatus(m.FromStatus),
		ToStatus:   domain.VideoTaskStatus(m.ToStatus),
		Message:    m.Message,
		CreatedAt:  m.CreatedAt.Unix(),
	}
}

// Database 数据库封装
type Database struct {
	DB *gorm.DB
}

// NewDatabase 创建数据库实例
func NewDatabase(db *gorm.DB) *Database {
	return &Database{DB: db}
}

// AutoMigrate 自动迁移表结构
func (d *Database) AutoMigrate() error {
	return d.DB.AutoMigrate(
		&VideoTaskModel{},
		&TaskStatusLogModel{},
	)
}
