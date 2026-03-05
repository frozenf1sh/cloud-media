package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/frozenf1sh/cloud-media/pkg/health"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/frozenf1sh/cloud-media/pkg/metrics"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
)

func main() {
	// 1. 先初始化一个简单日志（用于启动阶段）
	logger.InitSimple("info")

	// 2. 加载配置
	cfg, err := config.Load("")
	if err != nil {
		logger.Error("Failed to load config, using defaults", logger.Err(err))
		cfg = config.Default()
	}

	// 迁移旧版配置（向后兼容）
	cfg.MigrateLegacyConfig()

	// 3. 重新初始化日志（用完整配置）
	logger.Init(logger.Config{
		Level:          cfg.Log.Level,
		Format:         cfg.Log.Format,
		ServiceName:    cfg.Observability.ServiceName + "-worker",
		ServiceVersion: cfg.Observability.ServiceVersion,
	})
	logger.Info("Logger initialized",
		logger.String("level", cfg.Log.Level),
		logger.String("service", cfg.Observability.ServiceName),
		logger.String("version", cfg.Observability.ServiceVersion),
	)

	// 3. 初始化 OpenTelemetry
	ctx := context.Background()
	tracerProvider, err := telemetry.NewTracerProvider(ctx, telemetry.Config{
		ServiceName:    cfg.Observability.ServiceName + "-worker",
		ServiceVersion: cfg.Observability.ServiceVersion,
		Enabled:        cfg.Observability.Tracing.Enabled,
		Exporter:       cfg.Observability.Tracing.Exporter,
		OTLPEndpoint:   cfg.Observability.Tracing.OTLPEndpoint,
		Sampler:        cfg.Observability.Tracing.Sampler,
		SamplerRatio:   cfg.Observability.Tracing.SamplerRatio,
	})
	if err != nil {
		logger.Warn("Failed to initialize tracer provider, using noop", logger.Err(err))
	}
	defer func() {
		if tracerProvider != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = tracerProvider.Shutdown(shutdownCtx)
		}
	}()

	// 4. 设置信号处理
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 5. 使用 Wire 生成的依赖注入函数
	worker, err := InitializeWorker(cfg)
	if err != nil {
		logger.Error("Failed to initialize worker", logger.Err(err))
		panic(err)
	}

	// 6. 启动 metrics 和健康检查 HTTP 服务器（如果启用）
	if cfg.Observability.Metrics.Enabled {
		go func() {
			mux := http.NewServeMux()
			mux.Handle(cfg.Observability.Metrics.Path, metrics.HTTPHandler())
			mux.Handle("/health/live", health.LivenessHandler())
			mux.Handle("/health/ready", worker.HealthChecker().HTTPHandler())

			addr := cfg.Observability.Metrics.Address()
			logger.Info("Worker metrics/health server starting", logger.String("addr", addr))
			if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
				logger.Error("Worker metrics server failed", logger.Err(err))
			}
		}()
	}

	// 7. 启动 worker
	logger.Info("Worker starting...")

	// 运行 worker in goroutine
	workerErrChan := make(chan error, 1)
	go func() {
		workerErrChan <- worker.Run(ctx)
	}()

	// 等待信号或 worker 错误
	select {
	case <-sigChan:
		logger.Info("Received shutdown signal")
		cancel()
	case err := <-workerErrChan:
		if err != nil && err != context.Canceled {
			logger.Error("Worker failed", logger.Err(err))
			panic(err)
		}
	}

	logger.Info("Worker shutdown completed")
}
