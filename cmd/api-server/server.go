package main

import (
	"log/slog"
	"net/http"

	"github.com/frozenf1sh/cloud-media/internal/adapter/rpc"
	"github.com/frozenf1sh/cloud-media/internal/infrastructure/persistence"
	v1connect "github.com/frozenf1sh/cloud-media/proto/gen/api/v1/v1connect"
)

// Server 持有 HTTP handler 和路径
type Server struct {
	Path      string
	Handler   http.Handler
	Database  *persistence.Database
}

// NewServer 创建服务器
func NewServer(videoServer *rpc.VideoServer, db *persistence.Database) *Server {
	path, handler := v1connect.NewVideoServiceHandler(videoServer)

	// 执行自动迁移
	slog.Info("Running database migration...")
	if err := db.AutoMigrate(); err != nil {
		slog.Error("Failed to run migration", "error", err)
		panic(err)
	}
	slog.Info("Migration completed successfully")

	return &Server{
		Path:     path,
		Handler:  handler,
		Database: db,
	}
}
