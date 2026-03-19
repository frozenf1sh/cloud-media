// Package config 提供应用配置管理，支持 YAML 文件和环境变量。
//
// 配置优先级（从高到低）：
//  1. 环境变量（前缀 CLOUD_MEDIA_）
//  2. YAML 配置文件
//  3. 默认值
//
// 环境变量示例：
//
//	CLOUD_MEDIA_DATABASE_HOST=localhost
//	CLOUD_MEDIA_SERVER_PORT=8080
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/viper"
)

// ServerConfig HTTP 服务器配置
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // json, text
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
}

// RabbitMQConfig RabbitMQ 配置
type RabbitMQConfig struct {
	URL string `mapstructure:"url"`
}

// ObjectStorageType 对象存储类型
type ObjectStorageType string

const (
	ObjectStorageTypeMinIO ObjectStorageType = "minio"
	ObjectStorageTypeS3    ObjectStorageType = "s3"
	ObjectStorageTypeR2    ObjectStorageType = "r2"
)

// ObjectStorageConfig 对象存储配置（支持多种后端）
type ObjectStorageConfig struct {
	// Type 存储类型: minio, s3, r2
	Type ObjectStorageType `mapstructure:"type"`

	// Endpoint 配置
	InternalEndpoint string `mapstructure:"internal_endpoint"`
	InternalUseSSL   bool   `mapstructure:"internal_use_ssl"`
	ExternalEndpoint string `mapstructure:"external_endpoint"`
	ExternalUseSSL   bool   `mapstructure:"external_use_ssl"`

	// 认证配置
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`

	// Region 配置 (R2 用 "auto")
	Region string `mapstructure:"region"`

	// CDN 配置（可选，用于替代预签名 URL）
	CDN CDNConfig `mapstructure:"cdn"`
}

// CDNConfig CDN 配置
type CDNConfig struct {
	// Enabled 是否启用 CDN
	Enabled bool `mapstructure:"enabled"`
	// BaseURL CDN 基础域名，如 "https://cdn.example.com"
	BaseURL string `mapstructure:"base_url"`
	// URLSigningSecret CDN URL 签名密钥（可选）
	URLSigningSecret string `mapstructure:"url_signing_secret"`
}

// TranscoderVariantConfig 转码变体配置
type TranscoderVariantConfig struct {
	// Name 变体名称，如 "1080p"
	Name string `mapstructure:"name"`
	// TargetSize 目标尺寸（横屏为高度，竖屏为宽度）
	TargetSize int `mapstructure:"target_size"`
	// Bitrate 视频码率，如 "4000k"
	Bitrate string `mapstructure:"bitrate"`
	// Bandwidth HLS 播放列表带宽（bps）
	Bandwidth int `mapstructure:"bandwidth"`
}

// TranscoderConfig 转码器配置
type TranscoderConfig struct {
	// OutputBucket 输出存储桶
	OutputBucket string `mapstructure:"output_bucket"`
	// HLSTime HLS 分片时长（秒）
	HLSTime int `mapstructure:"hls_time"`
	// VideoCodec 视频编码器
	VideoCodec string `mapstructure:"video_codec"`
	// AudioCodec 音频编码器
	AudioCodec string `mapstructure:"audio_codec"`
	// AudioBitrate 音频码率
	AudioBitrate string `mapstructure:"audio_bitrate"`
	// Preset 编码预设（ultrafast, superfast, veryfast, faster, fast, medium, slow, slower, veryslow）
	Preset string `mapstructure:"preset"`
	// GOPSize 关键帧间隔（帧数）
	GOPSize int `mapstructure:"gop_size"`
	// ThumbnailSize 封面最大尺寸
	ThumbnailSize int `mapstructure:"thumbnail_size"`
	// ThreadCount FFmpeg 使用的线程数（0=自动）
	ThreadCount int `mapstructure:"thread_count"`
	// TimeoutMultiplier 超时倍数（视频时长 × 此倍数）
	TimeoutMultiplier float64 `mapstructure:"timeout_multiplier"`
	// MinTimeout 最小超时时间（分钟）
	MinTimeout int `mapstructure:"min_timeout"`
	// MaxTimeout 最大超时时间（分钟）
	MaxTimeout int `mapstructure:"max_timeout"`
	// Variants 多码率变体配置
	Variants []TranscoderVariantConfig `mapstructure:"variants"`
}

// OutboxConfig Outbox 服务配置
type OutboxConfig struct {
	// RecoveryInterval 恢复扫描间隔（秒）
	RecoveryInterval int `mapstructure:"recovery_interval"`
	// PendingTaskMaxAge 待处理任务最大存活时间（分钟）
	PendingTaskMaxAge int `mapstructure:"pending_task_max_age"`
	// BatchSize 批量处理大小
	BatchSize int `mapstructure:"batch_size"`
	// MaxRetries 最大重试次数
	MaxRetries int `mapstructure:"max_retries"`
}

// Config 应用配置
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Log           LogConfig           `mapstructure:"log"`
	Database      DatabaseConfig      `mapstructure:"database"`
	RabbitMQ      RabbitMQConfig      `mapstructure:"rabbitmq"`
	ObjectStorage ObjectStorageConfig `mapstructure:"object_storage"`
	Transcoder    TranscoderConfig    `mapstructure:"transcoder"`
	Outbox        OutboxConfig        `mapstructure:"outbox"`
	Observability ObservabilityConfig `mapstructure:"observability"`
}

// ObservabilityConfig 可观测性配置
type ObservabilityConfig struct {
	ServiceName    string        `mapstructure:"service_name"`
	ServiceVersion string        `mapstructure:"service_version"`
	Metrics        MetricsConfig `mapstructure:"metrics"`
	Tracing        TracingConfig `mapstructure:"tracing"`
}

// MetricsConfig Metrics 配置
type MetricsConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	Path         string `mapstructure:"path"`          // 保留用于向后兼容
	Port         int    `mapstructure:"port"`          // 保留用于向后兼容
	Exporter     string `mapstructure:"exporter"`      // otlp, stdout, none
	OTLPEndpoint string `mapstructure:"otlp_endpoint"` // OTLP 接收端地址
}

// TracingConfig OpenTelemetry 追踪配置
type TracingConfig struct {
	Enabled      bool    `mapstructure:"enabled"`
	Exporter     string  `mapstructure:"exporter"`      // otlp, stdout, none
	OTLPEndpoint string  `mapstructure:"otlp_endpoint"` // OTLP 接收端地址
	Sampler      string  `mapstructure:"sampler"`       // always_on, always_off, traceidratio
	SamplerRatio float64 `mapstructure:"sampler_ratio"`
}

// Load 从文件加载配置，同时支持环境变量覆盖
func Load(filePath string) (*Config, error) {
	v := viper.New()

	// 设置默认值
	setDefaults(v)

	// 配置环境变量
	v.SetEnvPrefix("CLOUD_MEDIA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 配置文件设置
	if filePath != "" {
		slog.Info("Loading config from specified path", "path", filePath)
		v.SetConfigFile(filePath)
	} else {
		slog.Info("Searching for config file in default paths")
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/app")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/cloud-media")
	}

	// 读取配置文件（可选，如果不存在也不报错）
	if err := v.ReadInConfig(); err != nil {
		slog.Warn("Failed to read config file, using defaults + env vars", "error", err)
	} else {
		slog.Info("Config file loaded successfully", "path", v.ConfigFileUsed())
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

// LoadFromEnv 仅从环境变量加载配置
func LoadFromEnv() (*Config, error) {
	v := viper.New()
	setDefaults(v)
	v.SetEnvPrefix("CLOUD_MEDIA")
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

// Default 返回默认配置
func Default() *Config {
	v := viper.New()
	setDefaults(v)
	var cfg Config
	_ = v.Unmarshal(&cfg)
	return &cfg
}

// setDefaults 设置所有配置项的默认值
func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "postgres")
	v.SetDefault("database.password", "password")
	v.SetDefault("database.dbname", "media-db")
	v.SetDefault("database.sslmode", "disable")

	v.SetDefault("rabbitmq.url", "amqp://guest:guest@localhost:5672/")

	// 对象存储默认配置
	v.SetDefault("object_storage.type", "minio")
	v.SetDefault("object_storage.internal_endpoint", "localhost:9000")
	v.SetDefault("object_storage.internal_use_ssl", false)
	v.SetDefault("object_storage.external_endpoint", "localhost:9000")
	v.SetDefault("object_storage.external_use_ssl", false)
	v.SetDefault("object_storage.access_key_id", "rootadmin")
	v.SetDefault("object_storage.secret_access_key", "rootpassword")
	v.SetDefault("object_storage.region", "us-east-1")

	// CDN 默认配置（禁用）
	v.SetDefault("object_storage.cdn.enabled", false)
	v.SetDefault("object_storage.cdn.base_url", "")

	// 转码器默认配置
	v.SetDefault("transcoder.output_bucket", "media-output")
	v.SetDefault("transcoder.hls_time", 6)
	v.SetDefault("transcoder.video_codec", "libx264")
	v.SetDefault("transcoder.audio_codec", "aac")
	v.SetDefault("transcoder.audio_bitrate", "128k")
	v.SetDefault("transcoder.preset", "fast")
	v.SetDefault("transcoder.gop_size", 48)
	v.SetDefault("transcoder.thumbnail_size", 1080)
	v.SetDefault("transcoder.thread_count", 0) // 0 = 自动
	v.SetDefault("transcoder.timeout_multiplier", 3.0)
	v.SetDefault("transcoder.min_timeout", 10)
	v.SetDefault("transcoder.max_timeout", 120)
	v.SetDefault("transcoder.variants", []map[string]any{
		{"name": "1080p", "target_size": 1080, "bitrate": "4000k", "bandwidth": 4000000},
		{"name": "720p", "target_size": 720, "bitrate": "2000k", "bandwidth": 2000000},
		{"name": "480p", "target_size": 480, "bitrate": "1000k", "bandwidth": 1000000},
	})

	// 可观测性默认配置
	v.SetDefault("observability.service_name", "cloud-media")
	v.SetDefault("observability.service_version", "1.0.0")

	v.SetDefault("observability.metrics.enabled", true)
	v.SetDefault("observability.metrics.path", "/metrics")
	v.SetDefault("observability.metrics.port", 9090)
	v.SetDefault("observability.metrics.exporter", "otlp")
	v.SetDefault("observability.metrics.otlp_endpoint", "localhost:4317")

	v.SetDefault("observability.tracing.enabled", true)
	v.SetDefault("observability.tracing.exporter", "otlp")
	v.SetDefault("observability.tracing.otlp_endpoint", "localhost:4317")
	v.SetDefault("observability.tracing.sampler", "always_on")
	v.SetDefault("observability.tracing.sampler_ratio", 1.0)

	// Outbox 默认配置
	v.SetDefault("outbox.recovery_interval", 30)
	v.SetDefault("outbox.pending_task_max_age", 60)
	v.SetDefault("outbox.batch_size", 10)
	v.SetDefault("outbox.max_retries", 10)
}

// Address 返回服务器监听地址
func (s *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// MetricsAddress 返回 Prometheus 指标监听地址
func (m *MetricsConfig) Address() string {
	return fmt.Sprintf(":%d", m.Port)
}

// Dump 返回配置的 JSON 字符串（用于调试，会隐藏敏感信息）
func (c *Config) Dump() string {
	// 创建一个副本，隐藏敏感信息
	safeCfg := *c
	safeCfg.Database.Password = "***REDACTED***"
	safeCfg.ObjectStorage.SecretAccessKey = "***REDACTED***"
	safeCfg.ObjectStorage.AccessKeyID = maskString(safeCfg.ObjectStorage.AccessKeyID)

	data, err := json.MarshalIndent(safeCfg, "", "  ")
	if err != nil {
		return fmt.Sprintf("failed to dump config: %v", err)
	}
	return string(data)
}

// maskString 部分隐藏敏感字符串
func maskString(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:2] + "..." + s[len(s)-2:]
}
