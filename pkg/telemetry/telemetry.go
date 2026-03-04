package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config 追踪配置
type Config struct {
	ServiceName    string
	ServiceVersion string
	Enabled        bool
	Exporter       string  // otlp, stdout, none
	OTLPEndpoint   string  // OTLP 接收端地址
	Sampler        string  // always_on, always_off, traceidratio
	SamplerRatio   float64
}

// TracerProvider 是对 OpenTelemetry TracerProvider 的包装
type TracerProvider struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	config   Config
}

// NewTracerProvider 创建新的追踪提供器
func NewTracerProvider(ctx context.Context, cfg Config) (*TracerProvider, error) {
	if !cfg.Enabled {
		slog.Info("Tracing is disabled")
		return &TracerProvider{
			provider: nil,
			tracer:   otel.Tracer("noop"),
			config:   cfg,
		}, nil
	}

	// 创建资源
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("environment", getEnvironment()),
		),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// 创建采样器
	sampler, err := createSampler(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create sampler: %w", err)
	}

	// 创建导出器
	exporter, err := createExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// 创建 TracerProvider
	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	// 设置全局 TracerProvider
	otel.SetTracerProvider(provider)

	// 设置全局 TextMapPropagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &TracerProvider{
		provider: provider,
		tracer:   provider.Tracer(cfg.ServiceName),
		config:   cfg,
	}, nil
}

// Tracer 返回 tracer
func (tp *TracerProvider) Tracer() trace.Tracer {
	return tp.tracer
}

// Shutdown 关闭追踪提供器
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp.provider == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := tp.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}

	slog.Info("Tracer provider shutdown completed")
	return nil
}

// StartSpan 开始一个 span
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer("cloud-media").Start(ctx, name, trace.WithAttributes(attrs...))
}

// SpanFromContext 从上下文获取 span
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// TraceIDFromContext 从上下文获取 trace ID
func TraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().HasTraceID() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// WithTraceID 向上下文中添加 trace（通过创建一个新的 span）
func WithTraceID(ctx context.Context, traceID string) context.Context {
	// 如果已经有 trace，直接返回
	if trace.SpanFromContext(ctx).SpanContext().HasTraceID() {
		return ctx
	}

	// 尝试从字符串解析 trace ID
	if tid, err := trace.TraceIDFromHex(traceID); err == nil {
		// 创建一个假的 span context
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: tid,
			SpanID:  [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
			Remote:  true,
		})
		return trace.ContextWithSpanContext(ctx, sc)
	}

	return ctx
}

// ExtractFromCarrier 从 carrier 提取 trace 上下文
func ExtractFromCarrier(ctx context.Context, carrier map[string]string) context.Context {
	propagator := otel.GetTextMapPropagator()
	return propagator.Extract(ctx, propagation.MapCarrier(carrier))
}

// InjectToCarrier 将 trace 上下文注入到 carrier
func InjectToCarrier(ctx context.Context, carrier map[string]string) {
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.MapCarrier(carrier))
}

// AddEvent 向当前 span 添加事件
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes 向当前 span 设置属性
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrs...)
}

// RecordError 向当前 span 记录错误
func RecordError(ctx context.Context, err error, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err, trace.WithAttributes(attrs...))
}

// String 创建字符串属性
func String(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

// Int 创建整数属性
func Int(key string, value int) attribute.KeyValue {
	return attribute.Int(key, value)
}

// Int64 创建 int64 属性
func Int64(key string, value int64) attribute.KeyValue {
	return attribute.Int64(key, value)
}

// createSampler 创建采样器
func createSampler(cfg Config) (sdktrace.Sampler, error) {
	switch cfg.Sampler {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(cfg.SamplerRatio), nil
	default:
		return nil, fmt.Errorf("unknown sampler: %s", cfg.Sampler)
	}
}

// createExporter 创建导出器
func createExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch cfg.Exporter {
	case "otlp":
		return createOTLPExporter(ctx, cfg.OTLPEndpoint)
	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "none":
		return nil, errors.New("exporter disabled")
	default:
		return nil, fmt.Errorf("unknown exporter: %s", cfg.Exporter)
	}
}

// createOTLPExporter 创建 OTLP 导出器
func createOTLPExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	slog.Info("OTLP exporter initialized", "endpoint", endpoint)
	return exporter, nil
}

// getEnvironment 获取环境名称
func getEnvironment() string {
	if env := os.Getenv("ENVIRONMENT"); env != "" {
		return env
	}
	if env := os.Getenv("ENV"); env != "" {
		return env
	}
	return "development"
}
