package main

import (
	"context"
	"net/http"

	"github.com/frozenf1sh/cloud-media/internal/adapter/rpc"
	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	"github.com/frozenf1sh/cloud-media/pkg/health"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	v1connect "github.com/frozenf1sh/cloud-media/proto/gen/api/v1/v1connect"
)

// Server 持有 HTTP handler 和路径
type Server struct {
	Path     string
	Handler  http.Handler
	Database *persistence.Database
	Health   *health.Health
}

// NewServer 创建服务器
func NewServer(videoServer *rpc.VideoServer, db *persistence.Database, storage domain.ObjectStorage) *Server {
	path, handler := v1connect.NewVideoServiceHandler(videoServer)

	// 执行自动迁移
	logger.Info("Running database migration...")
	if err := db.AutoMigrate(); err != nil {
		logger.Error("Failed to run migration", logger.Err(err))
		panic(err)
	}
	logger.Info("Migration completed successfully")

	// 确保必要的 bucket 存在
	ctx := context.Background()
	buckets := []string{"media-input", "media-output"}
	for _, bucket := range buckets {
		logger.Info("Ensuring bucket exists", logger.String("bucket", bucket))
		if err := storage.EnsureBucketExists(ctx, bucket); err != nil {
			logger.Warn("Failed to ensure bucket exists",
				logger.String("bucket", bucket),
				logger.Err(err))
		}
	}

	// 创建健康检查管理器
	healthChecker := health.New("api-server", "1.0.0")

	// 添加数据库健康检查
	healthChecker.RegisterFunc("database", health.SimpleCheck(func(ctx context.Context) error {
		sqlDB, err := db.DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	}))

	return &Server{
		Path:     path,
		Handler:  handler,
		Database: db,
		Health:   healthChecker,
	}
}
