//go:build wireinject
// +build wireinject

package main

import (
	"github.com/frozenf1sh/cloud-media/internal/adapter/rpc"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/broker"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
	"github.com/google/wire"
)

var handlerProviderSet = wire.NewSet(
	broker.ProviderSet,
	persistence.ProviderSet,
	persistence.RepositoryProviderSet,
	usecase.ProviderSet,
	rpc.ProviderSet,
	provideRabbitMQURL,
	provideDatabaseConfig,
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
