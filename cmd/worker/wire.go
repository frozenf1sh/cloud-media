//go:build wireinject
// +build wireinject

package main

import (
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/broker"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/storage"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/transcoder"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/google/wire"
)

var workerProviderSet = wire.NewSet(
	broker.ProviderSet,
	persistence.ProviderSet,
	persistence.RepositoryProviderSet,
	storage.ProviderSet,
	transcoder.ProviderSet,
	usecase.ProviderSet,
	NewWorker,
	provideRabbitMQURL,
	provideDatabaseConfig,
	provideMinIOConfig,
)

func InitializeWorker(cfg *config.Config) (*Worker, error) {
	wire.Build(workerProviderSet)
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

func provideMinIOConfig(cfg *config.Config) *storage.Config {
	return &storage.Config{
		InternalEndpoint: cfg.MinIO.InternalEndpoint,
		InternalUseSSL:  cfg.MinIO.InternalUseSSL,
		ExternalEndpoint: cfg.MinIO.ExternalEndpoint,
		ExternalUseSSL:  cfg.MinIO.ExternalUseSSL,
		AccessKeyID:     cfg.MinIO.AccessKeyID,
		SecretAccessKey: cfg.MinIO.SecretAccessKey,
	}
}
