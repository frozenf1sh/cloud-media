//go:build wireinject
// +build wireinject

package main

import (
	"github.com/frozenf1sh/cloud-media/internal/adapter/rpc"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/broker"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/storage"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
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
	provideMinIOConfig,
)

func InitializeVideoServer() (*Server, error) {
	wire.Build(handlerProviderSet, NewServer)
	return nil, nil
}

func provideRabbitMQURL() string {
	return "amqp://guest:guest@localhost:5672/"
}

func provideDatabaseConfig() *persistence.Config {
	return persistence.NewDefaultConfig()
}

func provideMinIOConfig() *storage.Config {
	return &storage.Config{
		InternalEndpoint: "localhost:9000",
		InternalUseSSL:  false,
		ExternalEndpoint: "localhost:9000",
		ExternalUseSSL:  false,
		AccessKeyID:     "rootadmin",
		SecretAccessKey: "rootpassword",
	}
}
