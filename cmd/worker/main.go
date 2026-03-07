
package main

import (
	"context"
	"encoding/json"
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
	logger.InitSimple("info")

	cfg, err := config.Load("")
	if err != nil {
		logger.Error("Failed to load config, using defaults", logger.Err(err))
		cfg = config.Default()
	}

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

	// 打印配置（DEBUG级别）
	logger.Debug("Loaded configuration", logger.String("config", cfg.Dump()))

	ctx := context.Background()
	tracerProvider, err := telemetry.NewTracerProvider(ctx, telemetry.Config{
		ServiceName:    cfg.Observability.ServiceName,
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

	metricsProvider, err := metrics.NewMetricsProvider(ctx, metrics.Config{
		ServiceName:    cfg.Observability.ServiceName,
		ServiceVersion: cfg.Observability.ServiceVersion,
		Enabled:        cfg.Observability.Metrics.Enabled,
		Exporter:       cfg.Observability.Metrics.Exporter,
		OTLPEndpoint:   cfg.Observability.Metrics.OTLPEndpoint,
	})
	if err != nil {
		logger.Warn("Failed to initialize metrics provider, skipping metrics", logger.Err(err))
	}
	defer func() {
		if metricsProvider != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = metricsProvider.Shutdown(shutdownCtx)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	worker, err := InitializeWorker(cfg)
	if err != nil {
		logger.Error("Failed to initialize worker", logger.Err(err))
		panic(err)
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/health/live", health.LivenessHandler())
		mux.Handle("/health/ready", worker.HealthChecker().HTTPHandler())

		// 添加状态端点，用于 preStop hook 检查
		mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			status := WorkerStatus{
				ActiveTasks: worker.ActiveTaskCount(),
			}
			_ = json.NewEncoder(w).Encode(status)
		})

		addr := cfg.Server.Address()
		logger.Info("Worker health server starting", logger.String("addr", addr))
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			logger.Error("Worker health server failed", logger.Err(err))
		}
	}()

	logger.Info("Worker starting...")

	workerErrChan := make(chan error, 1)
	go func() {
		workerErrChan <- worker.Run(ctx)
	}()

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

