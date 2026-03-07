
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config metrics 配置
type Config struct {
	ServiceName    string
	ServiceVersion string
	Enabled        bool
	Exporter       string
	OTLPEndpoint   string
}

// MetricsProvider metrics 提供者
type MetricsProvider struct {
	config        Config
	meter         metric.Meter
	meterProvider *sdkmetric.MeterProvider
	shutdownOnce  sync.Once

	apiRequestsTotal   metric.Int64Counter
	apiRequestDuration metric.Float64Histogram
	tasksTotal         metric.Int64Counter
	taskDuration       metric.Float64Histogram
	tasksInProgress    metric.Int64UpDownCounter
	queueSize          metric.Int64Gauge
	transcodedBytes    metric.Int64Counter
	transcodedVideos   metric.Int64Counter
}

var (
	globalProvider *MetricsProvider
	providerMutex  sync.RWMutex
)

// NewMetricsProvider 创建新的 metrics 提供者
func NewMetricsProvider(ctx context.Context, cfg Config) (*MetricsProvider, error) {
	provider := &MetricsProvider{
		config: cfg,
	}

	if !cfg.Enabled {
		slog.Info("Metrics is disabled")
		globalProvider = provider
		return provider, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		slog.Warn("Failed to create metrics resource, metrics will be disabled", "error", err)
		globalProvider = provider
		return provider, nil
	}

	if cfg.Exporter == "otlp" || cfg.Exporter == "stdout" {
		if err := provider.initExporter(ctx, res, cfg); err != nil {
			slog.Warn("Failed to init metrics exporter, metrics will be disabled", "error", err)
			// 即使 exporter 初始化失败也返回 provider，只是不启用实际 metrics
			globalProvider = provider
			return provider, nil
		}
	}

	providerMutex.Lock()
	globalProvider = provider
	providerMutex.Unlock()

	slog.Info("Metrics provider initialized",
		"exporter", cfg.Exporter,
		"otlp_endpoint", cfg.OTLPEndpoint,
		"service_name", cfg.ServiceName)

	return provider, nil
}

func (p *MetricsProvider) initExporter(ctx context.Context, res *resource.Resource, cfg Config) error {
	var reader sdkmetric.Reader

	if cfg.Exporter == "otlp" {
		exporter, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlpmetricgrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			otlpmetricgrpc.WithInsecure(),
			otlpmetricgrpc.WithReconnectionPeriod(5*time.Second),
		)
		if err != nil {
			slog.Warn("Failed to create OTLP metric exporter, metrics will operate without exporting", "error", err, "endpoint", cfg.OTLPEndpoint)
			// 使用空 reader，不导出 metrics 但保持 API 可用
			reader = sdkmetric.NewPeriodicReader(nil, sdkmetric.WithInterval(60*time.Second))
		} else {
			reader = sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(15*time.Second))
			slog.Info("OTLP metrics exporter initialized", "endpoint", cfg.OTLPEndpoint)
		}
	} else {
		reader = sdkmetric.NewPeriodicReader(nil, sdkmetric.WithInterval(60*time.Second))
	}

	p.meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)
	otel.SetMeterProvider(p.meterProvider)
	p.meter = p.meterProvider.Meter(p.config.ServiceName)

	if err := p.initMetrics(); err != nil {
		return err
	}

	return nil
}

func (p *MetricsProvider) initMetrics() error {
	var err error

	if p.apiRequestsTotal, err = p.meter.Int64Counter(
		"api_requests_total",
		metric.WithDescription("Total number of API requests"),
		metric.WithUnit("1"),
	); err != nil {
		return err
	}

	if p.apiRequestDuration, err = p.meter.Float64Histogram(
		"api_request_duration_seconds",
		metric.WithDescription("API request duration in seconds"),
		metric.WithUnit("s"),
	); err != nil {
		return err
	}

	if p.tasksTotal, err = p.meter.Int64Counter(
		"tasks_total",
		metric.WithDescription("Total number of tasks processed"),
		metric.WithUnit("1"),
	); err != nil {
		return err
	}

	if p.taskDuration, err = p.meter.Float64Histogram(
		"task_duration_seconds",
		metric.WithDescription("Task processing duration in seconds"),
		metric.WithUnit("s"),
	); err != nil {
		return err
	}

	if p.tasksInProgress, err = p.meter.Int64UpDownCounter(
		"tasks_in_progress",
		metric.WithDescription("Number of tasks currently being processed"),
		metric.WithUnit("1"),
	); err != nil {
		return err
	}

	if p.queueSize, err = p.meter.Int64Gauge(
		"queue_size",
		metric.WithDescription("Number of tasks waiting in queue"),
		metric.WithUnit("1"),
	); err != nil {
		return err
	}

	if p.transcodedBytes, err = p.meter.Int64Counter(
		"transcoded_bytes_total",
		metric.WithDescription("Total bytes transcoded"),
		metric.WithUnit("By"),
	); err != nil {
		return err
	}

	if p.transcodedVideos, err = p.meter.Int64Counter(
		"transcoded_videos_total",
		metric.WithDescription("Total number of videos transcoded"),
		metric.WithUnit("1"),
	); err != nil {
		return err
	}

	return nil
}

func (p *MetricsProvider) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		if p.meterProvider != nil {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			if err := p.meterProvider.Shutdown(ctx); err != nil {
				slog.Error("Failed to shutdown meter provider", "error", err)
			} else {
				slog.Info("Metrics provider shutdown completed")
			}
		}
	})
	return nil
}

func GlobalProvider() *MetricsProvider {
	providerMutex.RLock()
	defer providerMutex.RUnlock()
	return globalProvider
}

func RecordAPIRequest(method, path string, status int, duration time.Duration) {
	statusStr := fmt.Sprintf("%d", status)
	if provider := GlobalProvider(); provider != nil && provider.apiRequestsTotal != nil {
		ctx := context.Background()
		provider.apiRequestsTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("method", method),
				attribute.String("path", path),
				attribute.String("status", statusStr),
			),
		)
		provider.apiRequestDuration.Record(ctx, duration.Seconds(),
			metric.WithAttributes(
				attribute.String("method", method),
				attribute.String("path", path),
			),
		)
	}
}

func RecordTaskStarted() {
	if provider := GlobalProvider(); provider != nil && provider.tasksInProgress != nil {
		provider.tasksInProgress.Add(context.Background(), 1)
	}
}

func RecordTaskCompleted(status string, duration time.Duration) {
	if provider := GlobalProvider(); provider != nil {
		ctx := context.Background()
		if provider.tasksInProgress != nil {
			provider.tasksInProgress.Add(ctx, -1)
		}
		if provider.tasksTotal != nil {
			provider.tasksTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("status", status)))
		}
		if provider.taskDuration != nil {
			provider.taskDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attribute.String("status", status)))
		}
	}
}

func SetQueueSize(size int) {
	if provider := GlobalProvider(); provider != nil && provider.queueSize != nil {
		provider.queueSize.Record(context.Background(), int64(size))
	}
}

func RecordTranscodedBytes(bytes int64) {
	if provider := GlobalProvider(); provider != nil && provider.transcodedBytes != nil {
		provider.transcodedBytes.Add(context.Background(), bytes)
	}
}

func RecordTranscodedVideo(quality string) {
	if provider := GlobalProvider(); provider != nil && provider.transcodedVideos != nil {
		provider.transcodedVideos.Add(context.Background(), 1, metric.WithAttributes(attribute.String("quality", quality)))
	}
}

