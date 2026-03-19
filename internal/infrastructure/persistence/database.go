// Package persistence 实现领域仓储接口，使用 GORM 访问 PostgreSQL。
//
// 包含：
//   - Database - 数据库连接和迁移
//   - VideoTaskRepository - 视频任务仓储实现
//   - OutboxRepository - Outbox 事件仓储实现
//   - ProcessedMessageRepository - 已处理消息仓储实现
package persistence

import (
	"fmt"
	"time"

	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/google/wire"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ProviderSet 是 Wire 的提供者集合（等实现 repository 后再加入）
var ProviderSet = wire.NewSet(
	NewGormDB,
	NewDatabase,
)

// Config 数据库配置
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
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
	// 先创建复合唯一索引
	if err := d.DB.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_processed_messages_msg_consumer
		ON processed_messages(message_id, consumer_id)
	`).Error; err != nil {
		// 索引可能已存在，忽略错误
	}

	return d.DB.AutoMigrate(
		&VideoTaskModel{},
		&TaskStatusLogModel{},
		&OutboxEventModel{},
		&ProcessedMessageModel{},
	)
}

// NewGormDB 创建 GORM 数据库连接
func NewGormDB(cfg *Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host,
		cfg.Port,
		cfg.User,
		cfg.Password,
		cfg.DBName,
		cfg.SSLMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.NewGormLogger(),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	// 设置连接池
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	return db, nil
}

// NewDefaultConfig 创建默认配置（用于本地开发）
func NewDefaultConfig() *Config {
	return &Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "password",
		DBName:   "media-db",
		SSLMode:  "disable",
	}
}
