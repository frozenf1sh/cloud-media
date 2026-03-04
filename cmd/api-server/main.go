package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/frozenf1sh/cloud-media/pkg/health"
	"github.com/frozenf1sh/cloud-media/pkg/interceptor"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/frozenf1sh/cloud-media/pkg/metrics"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("Failed to load config, using defaults", "error", err)
		cfg = config.Default()
	}

	// 2. 初始化日志
	logger.Init(logger.Config{
		Level:          cfg.Log.Level,
		Format:         cfg.Log.Format,
		ServiceName:    cfg.Observability.ServiceName,
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
		ServiceName:    cfg.Observability.ServiceName + "-api",
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

	// 4. 使用 Wire 生成的依赖注入函数
	server, err := InitializeVideoServer(cfg)
	if err != nil {
		logger.Error("Failed to initialize server", logger.Err(err))
		panic(err)
	}

	logger.Info("Service path", logger.String("path", server.Path))

	// 5. 注册路由并启动 HTTP 服务器
	mux := http.NewServeMux()
	mux.Handle(server.Path, server.Handler)

	// 注册健康检查端点
	mux.Handle("/health/live", health.LivenessHandler())
	mux.Handle("/health/ready", server.Health.HTTPHandler())

	// 注册 Prometheus metrics 端点
	if cfg.Observability.Metrics.Enabled {
		mux.Handle(cfg.Observability.Metrics.Path, metrics.HTTPHandler())
		logger.Info("Metrics endpoint registered", logger.String("path", cfg.Observability.Metrics.Path))
	}

	// 6. 使用追踪拦截器包装 mux
	handler := interceptor.TracingInterceptor(mux)

	// 7. 设置信号处理
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 8. 启动服务器
	addr := cfg.Server.Address()
	logger.Info("Server starting", logger.String("addr", addr))

	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// 启动服务器 goroutine
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server failed to start", logger.Err(err))
			panic(err)
		}
	}()

	// 等待信号
	<-sigChan
	logger.Info("Received shutdown signal")

	// 优雅关闭
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server shutdown failed", logger.Err(err))
	}

	logger.Info("Server shutdown completed")
}
