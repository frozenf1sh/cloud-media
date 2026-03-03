package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("Failed to load config, using defaults", "error", err)
		cfg = config.Default()
	}

	// 2. 初始化日志
	logger.Init(cfg.Log.Level)
	slog.Info("Logger initialized", "level", cfg.Log.Level)

	// 3. 设置信号处理
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received shutdown signal")
		cancel()
	}()

	// 4. 使用 Wire 生成的依赖注入函数
	worker, err := InitializeWorker(cfg)
	if err != nil {
		slog.Error("Failed to initialize worker", "error", err)
		panic(err)
	}

	// 5. 启动 worker
	slog.Info("Worker starting...")
	if err := worker.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("Worker failed", "error", err)
		panic(err)
	}

	slog.Info("Worker shutdown completed")
}
