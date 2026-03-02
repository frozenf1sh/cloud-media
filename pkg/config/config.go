package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config 应用配置
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Log      LogConfig      `mapstructure:"log"`
	Database DatabaseConfig `mapstructure:"database"`
	RabbitMQ RabbitMQConfig `mapstructure:"rabbitmq"`
	MinIO    MinIOConfig    `mapstructure:"minio"`
}

// ServerConfig HTTP 服务器配置
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `mapstructure:"level"` // debug, info, warn, error
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

// MinIOConfig MinIO 配置（双 Endpoint 模式）
type MinIOConfig struct {
	InternalEndpoint string `mapstructure:"internal_endpoint"`
	InternalUseSSL  bool   `mapstructure:"internal_use_ssl"`
	ExternalEndpoint string `mapstructure:"external_endpoint"`
	ExternalUseSSL  bool   `mapstructure:"external_use_ssl"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
}

// Load 从文件加载配置
func Load(filePath string) (*Config, error) {
	v := viper.New()

	// 设置默认值
	setDefaults(v)

	// 配置文件设置
	if filePath != "" {
		v.SetConfigFile(filePath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./configs")
		v.AddConfigPath("/etc/cloud-media")
	}

	// 读取配置文件
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// 支持环境变量覆盖
	v.AutomaticEnv()
	v.SetEnvPrefix("CLOUD_MEDIA")

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
	v.AutomaticEnv()
	v.SetEnvPrefix("CLOUD_MEDIA")

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

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)

	v.SetDefault("log.level", "info")

	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "postgres")
	v.SetDefault("database.password", "password")
	v.SetDefault("database.dbname", "media-db")
	v.SetDefault("database.sslmode", "disable")

	v.SetDefault("rabbitmq.url", "amqp://guest:guest@localhost:5672/")

	v.SetDefault("minio.internal_endpoint", "localhost:9000")
	v.SetDefault("minio.internal_use_ssl", false)
	v.SetDefault("minio.external_endpoint", "localhost:9000")
	v.SetDefault("minio.external_use_ssl", false)
	v.SetDefault("minio.access_key_id", "rootadmin")
	v.SetDefault("minio.secret_access_key", "rootpassword")
}

// Address 返回服务器监听地址
func (s *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}
