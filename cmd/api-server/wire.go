//go:build wireinject
// +build wireinject

package main

import (
	"time"

	"github.com/frozenf1sh/cloud-media/internal/adapter/rpc"
	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/broker"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/storage"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/google/wire"
)

var handlerProviderSet = wire.NewSet(
	persistence.ProviderSet,
	persistence.RepositoryProviderSet,
	storage.ProviderSet,
	usecase.ProviderSet,
	rpc.ProviderSet,
	provideRabbitMQBroker,
	wire.Bind(new(domain.ReliableMQBroker), new(*broker.RabbitMQBroker)),
	provideDatabaseConfig,
	provideObjectStorageConfig,
	provideOutboxConfig,
)

func InitializeVideoServer(cfg *config.Config) (*Server, error) {
	wire.Build(handlerProviderSet, NewServer)
	return nil, nil
}

func provideRabbitMQBroker(cfg *config.Config, msgRepo domain.ProcessedMessageRepository) (*broker.RabbitMQBroker, error) {
	return broker.NewRabbitMQBroker(cfg.RabbitMQ.URL, msgRepo)
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
