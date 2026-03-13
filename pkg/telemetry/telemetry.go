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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// 自定义属性键，用于标记健康检查请求
const attrKeyHealthCheck = "health_check"

// AttrHealthCheck 用于标记健康检查请求的属性
var AttrHealthCheck = attribute.Bool(attrKeyHealthCheck, true)

// healthCheckSampler 自定义采样器，用于过滤健康检查请求
type healthCheckSampler struct {
	base sdktrace.Sampler
}

// NewHealthCheckSampler 创建一个新的健康检查过滤采样器
func NewHealthCheckSampler(base sdktrace.Sampler) sdktrace.Sampler {
	return &healthCheckSampler{base: base}
}

// ShouldSample 实现 sdktrace.Sampler 接口
func (s *healthCheckSampler) ShouldSample(params sdktrace.SamplingParameters) sdktrace.SamplingResult {
	// 检查是否有健康检查属性
	for _, attr := range params.Attributes {
		if attr.Key == attrKeyHealthCheck && attr.Value.AsBool() {
			// 健康检查请求，直接 Drop
			return sdktrace.SamplingResult{
				Decision:   sdktrace.Drop,
				Attributes: params.Attributes,
			}
		}
	}
	// 非健康检查请求，委托给基础采样器
	return s.base.ShouldSample(params)
}

// Description 实现 sdktrace.Sampler 接口
func (s *healthCheckSampler) Description() string {
	return fmt.Sprintf("HealthCheckSampler{base=%s}", s.base.Description())
}

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
		slog.Warn("Failed to create tracing resource, tracing will be disabled", "error", err)
		return &TracerProvider{
			provider: nil,
			tracer:   otel.Tracer("noop"),
			config:   cfg,
		}, nil
	}

	// 创建采样器
	sampler, err := createSampler(cfg)
	if err != nil {
		slog.Warn("Failed to create sampler, tracing will be disabled", "error", err)
		return &TracerProvider{
			provider: nil,
			tracer:   otel.Tracer("noop"),
			config:   cfg,
		}, nil
	}
	slog.Info("Tracing sampler configured",
		"sampler_type", cfg.Sampler,
		"sampler_ratio", cfg.SamplerRatio)

	// 创建导出器
	exporter, err := createExporter(ctx, cfg)
	if err != nil {
		slog.Warn("Failed to create tracing exporter, tracing will be disabled", "error", err)
		return &TracerProvider{
			provider: nil,
			tracer:   otel.Tracer("noop"),
			config:   cfg,
		}, nil
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

// SpanIDFromContext 从上下文获取 span ID
func SpanIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().HasSpanID() {
		return span.SpanContext().SpanID().String()
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

// WithTraceSpanContext 向上下文中添加 trace 和 span
func WithTraceSpanContext(ctx context.Context, traceID, spanID string) context.Context {
	// 如果已经有 trace，直接返回（安全检查，防止意外覆盖）
	if trace.SpanFromContext(ctx).SpanContext().HasTraceID() {
		return ctx
	}

	var tid trace.TraceID
	var sid trace.SpanID
	var err error

	// 解析 trace ID
	if traceID != "" {
		tid, err = trace.TraceIDFromHex(traceID)
		if err != nil {
			return ctx
		}
	} else {
		return ctx
	}

	// 解析 span ID
	if spanID != "" {
		sid, err = trace.SpanIDFromHex(spanID)
		if err != nil {
			// 如果 span ID 解析失败，生成一个随机的
			sid = [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
		}
	} else {
		sid = [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	}

	// 创建 span context
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid,
		SpanID:  sid,
		Remote:  true,
	})
	return trace.ContextWithSpanContext(ctx, sc)
}

// ForceWithTraceSpanContext 强制向上下文中添加 trace 和 span（即使已有 trace）
// 注意：仅在明确需要覆盖时使用！
func ForceWithTraceSpanContext(ctx context.Context, traceID, spanID string) context.Context {
	var tid trace.TraceID
	var sid trace.SpanID
	var err error

	// 解析 trace ID
	if traceID != "" {
		tid, err = trace.TraceIDFromHex(traceID)
		if err != nil {
			return ctx
		}
	} else {
		return ctx
	}

	// 解析 span ID
	if spanID != "" {
		sid, err = trace.SpanIDFromHex(spanID)
		if err != nil {
			// 如果 span ID 解析失败，生成一个随机的
			sid = [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
		}
	} else {
		sid = [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	}

	// 创建 span context
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid,
		SpanID:  sid,
		Remote:  true,
	})
	return trace.ContextWithSpanContext(ctx, sc)
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

// RecordError 向当前 span 记录错误并设置 span 状态为 Error
func RecordError(ctx context.Context, err error, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err, trace.WithAttributes(attrs...))
	span.SetStatus(codes.Error, err.Error())
}

// SetSpanStatusOK 设置当前 span 状态为 OK
func SetSpanStatusOK(ctx context.Context) {
	span := trace.SpanFromContext(ctx)
	span.SetStatus(codes.Ok, "")
}

// SetSpanStatusError 手动设置当前 span 状态为 Error（不调用 RecordError）
func SetSpanStatusError(ctx context.Context, description string) {
	span := trace.SpanFromContext(ctx)
	span.SetStatus(codes.Error, description)
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
	var baseSampler sdktrace.Sampler
	switch cfg.Sampler {
	case "always_on":
		// 使用 ParentBased 包装，确保即使有父 span 也总是采样
		baseSampler = sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "always_off":
		baseSampler = sdktrace.ParentBased(sdktrace.NeverSample())
	case "traceidratio":
		baseSampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplerRatio))
	default:
		return nil, fmt.Errorf("unknown sampler: %s", cfg.Sampler)
	}
	// 使用健康检查过滤采样器包装基础采样器
	return NewHealthCheckSampler(baseSampler), nil
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

	// 使用非阻塞方式创建 OTLP 导出器，不等待连接建立
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithReconnectionPeriod(5*time.Second),
	)
	if err != nil {
		// 即使创建失败也不返回错误，使用 stdout 导出器作为备选
		slog.Warn("Failed to create OTLP trace exporter, falling back to no-op", "error", err, "endpoint", endpoint)
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	}

	slog.Info("OTLP trace exporter initialized", "endpoint", endpoint)
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
