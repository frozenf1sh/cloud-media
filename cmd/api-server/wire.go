//go:build wireinject
// +build wireinject

package main

import (
	"time"

	"github.com/frozenf1sh/cloud-media/internal/adapter/rpc"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/broker"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/storage"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/google/wire"
)

var handlerProviderSet = wire.NewSet(
	broker.ProviderSet,
	persistence.ProviderSet,
	persistence.RepositoryProviderSet,
	storage.ProviderSet,
	usecase.ProviderSet,
	rpc.ProviderSet,
	provideRabbitMQURL,
	provideDatabaseConfig,
	provideObjectStorageConfig,
	provideOutboxConfig,
)

func InitializeVideoServer(cfg *config.Config) (*Server, error) {
	wire.Build(handlerProviderSet, NewServer)
	return nil, nil
}

func provideRabbitMQURL(cfg *config.Config) string {
	return cfg.RabbitMQ.URL
}

func provideDatabaseConfig(cfg *config.Config) *persistence.Config {
	return &persistence.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		DBName:   cfg.Database.DBName,
		SSLMode:  cfg.Database.SSLMode,
	}
}

func provideObjectStorageConfig(cfg *config.Config) *config.ObjectStorageConfig {
	return &cfg.ObjectStorage
}

func provideOutboxConfig(cfg *config.Config) usecase.OutboxConfig {
	return usecase.OutboxConfig{
		RecoveryInterval:  time.Duration(cfg.Outbox.RecoveryInterval) * time.Second,
		PendingTaskMaxAge: time.Duration(cfg.Outbox.PendingTaskMaxAge) * time.Minute,
		BatchSize:         cfg.Outbox.BatchSize,
		MaxRetries:        cfg.Outbox.MaxRetries,
	}
}
