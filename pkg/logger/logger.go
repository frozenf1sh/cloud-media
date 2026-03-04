package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"go.opentelemetry.io/otel/trace"
)

// Key 是日志属性的键类型
type Key string

const (
	// TraceIDKey 是 trace ID 的键
	TraceIDKey Key = "trace_id"
	// SpanIDKey 是 span ID 的键
	SpanIDKey Key = "span_id"
	// ServiceKey 是服务名称的键
	ServiceKey Key = "service"
	// VersionKey 是版本的键
	VersionKey Key = "version"
	// ErrorKey 是错误的键
	ErrorKey Key = "error"
)

// Handler 是自定义的 slog.Handler，用于从上下文中提取 trace 信息并添加到日志中
type Handler struct {
	slog.Handler
	serviceName    string
	serviceVersion string
}

// NewHandler 创建一个新的 Handler 实例
func NewHandler(h slog.Handler, serviceName, serviceVersion string) *Handler {
	return &Handler{
		Handler:        h,
		serviceName:    serviceName,
		serviceVersion: serviceVersion,
	}
}

// Handle 方法实现了 slog.Handler 接口
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	// 添加服务信息
	if h.serviceName != "" {
		r.AddAttrs(slog.Attr{Key: string(ServiceKey), Value: slog.StringValue(h.serviceName)})
	}
	if h.serviceVersion != "" {
		r.AddAttrs(slog.Attr{Key: string(VersionKey), Value: slog.StringValue(h.serviceVersion)})
	}

	// 从上下文中提取 trace 信息
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		sc := span.SpanContext()
		r.AddAttrs(
			slog.Attr{Key: string(TraceIDKey), Value: slog.StringValue(sc.TraceID().String())},
			slog.Attr{Key: string(SpanIDKey), Value: slog.StringValue(sc.SpanID().String())},
		)
	}

	// 调用底层 Handler 处理日志
	return h.Handler.Handle(ctx, r)
}

// WithAttrs 返回一个带有指定属性的新 Handler
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		Handler:        h.Handler.WithAttrs(attrs),
		serviceName:    h.serviceName,
		serviceVersion: h.serviceVersion,
	}
}

// WithGroup 返回一个带有指定分组的新 Handler
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		Handler:        h.Handler.WithGroup(name),
		serviceName:    h.serviceName,
		serviceVersion: h.serviceVersion,
	}
}

// Config 日志配置
type Config struct {
	Level          string // debug, info, warn, error
	Format         string // json, text
	ServiceName    string
	ServiceVersion string
}

// Init 初始化日志系统
func Init(cfg Config) {
	// 解析日志级别
	var l slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     l,
		AddSource: true,
	}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// 使用自定义 Handler 包装
	wrappedHandler := NewHandler(handler, cfg.ServiceName, cfg.ServiceVersion)

	// 设置为默认 logger
	slog.SetDefault(slog.New(wrappedHandler))

	slog.Info("Logger initialized",
		"level", cfg.Level,
		"format", cfg.Format,
		"service", cfg.ServiceName,
		"version", cfg.ServiceVersion,
	)
}

// InitSimple 简单初始化（兼容旧接口）
func InitSimple(level string) {
	Init(Config{
		Level:          level,
		Format:         "json",
		ServiceName:    "cloud-media",
		ServiceVersion: "1.0.0",
	})
}

// 便捷日志函数

// Debug 记录 debug 级别日志
func Debug(msg string, attrs ...any) {
	slog.Debug(msg, attrs...)
}

// DebugContext 记录带上下文的 debug 级别日志
func DebugContext(ctx context.Context, msg string, attrs ...any) {
	slog.DebugContext(ctx, msg, attrs...)
}

// Info 记录 info 级别日志
func Info(msg string, attrs ...any) {
	slog.Info(msg, attrs...)
}

// InfoContext 记录带上下文的 info 级别日志
func InfoContext(ctx context.Context, msg string, attrs ...any) {
	slog.InfoContext(ctx, msg, attrs...)
}

// Warn 记录 warn 级别日志
func Warn(msg string, attrs ...any) {
	slog.Warn(msg, attrs...)
}

// WarnContext 记录带上下文的 warn 级别日志
func WarnContext(ctx context.Context, msg string, attrs ...any) {
	slog.WarnContext(ctx, msg, attrs...)
}

// Error 记录 error 级别日志
func Error(msg string, attrs ...any) {
	slog.Error(msg, attrs...)
}

// ErrorContext 记录带上下文的 error 级别日志
func ErrorContext(ctx context.Context, msg string, attrs ...any) {
	slog.ErrorContext(ctx, msg, attrs...)
}

// Err 创建 error 属性
func Err(err error) slog.Attr {
	return slog.Attr{Key: string(ErrorKey), Value: slog.AnyValue(err)}
}

// String 创建 string 属性
func String(key, value string) slog.Attr {
	return slog.String(key, value)
}

// Int 创建 int 属性
func Int(key string, value int) slog.Attr {
	return slog.Int(key, value)
}

// Int64 创建 int64 属性
func Int64(key string, value int64) slog.Attr {
	return slog.Int64(key, value)
}

// Float64 创建 float64 属性
func Float64(key string, value float64) slog.Attr {
	return slog.Float64(key, value)
}

// Bool 创建 bool 属性
func Bool(key string, value bool) slog.Attr {
	return slog.Bool(key, value)
}

// Any 创建任意类型属性
func Any(key string, value any) slog.Attr {
	return slog.Any(key, value)
}

// 兼容旧接口的函数

// FromContext 从上下文中提取 Trace ID
func FromContext(ctx context.Context) string {
	return telemetry.TraceIDFromContext(ctx)
}

// WithTraceID 向上下文中添加 Trace ID
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return telemetry.WithTraceID(ctx, traceID)
}
