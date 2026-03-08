package interceptor

import (
	"net/http"
	"time"

	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/frozenf1sh/cloud-media/pkg/metrics"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// TracingInterceptor 全链路追踪拦截器，用于 HTTP 请求
func TracingInterceptor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 从请求头中提取 trace 上下文
		ctx := telemetry.ExtractFromCarrier(r.Context(), headersToCarrier(r.Header))

		// 准备 span 属性
		attrs := []attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.URLPath(r.URL.Path),
			semconv.HTTPRoute(r.URL.Path),
			semconv.NetworkProtocolVersion(r.Proto),
			semconv.ClientAddress(r.RemoteAddr),
		}

		// 如果是健康检查，添加标记属性
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			attrs = append(attrs, telemetry.AttrHealthCheck)
		}

		// 开始一个新的 span
		spanName := r.Method + " " + r.URL.Path
		ctx, span := telemetry.StartSpan(ctx, spanName, attrs...)
		defer span.End()

		// 使用支持更多接口的包装器
		lrw := &loggingResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// 处理请求
		next.ServeHTTP(lrw, r.WithContext(ctx))

		duration := time.Since(start)

		// 健康检查请求不记录指标和日志
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			return
		}

		// 记录指标
		metrics.RecordAPIRequest(r.Method, r.URL.Path, lrw.statusCode, duration)

		// 设置 span 状态
		span.SetAttributes(
			semconv.HTTPResponseStatusCode(lrw.statusCode),
			telemetry.String("http.response.status", http.StatusText(lrw.statusCode)),
		)

		// 记录访问日志
		logger.InfoContext(ctx, "HTTP request completed",
			logger.String("method", r.Method),
			logger.String("path", r.URL.Path),
			logger.Int("status", lrw.statusCode),
			logger.String("status_text", http.StatusText(lrw.statusCode)),
			logger.Float64("duration_ms", float64(duration.Microseconds())/1000.0),
			logger.String("remote_addr", r.RemoteAddr),
		)

		// 注入 trace 信息到响应头
		carrier := make(map[string]string)
		telemetry.InjectToCarrier(ctx, carrier)
		for k, v := range carrier {
			w.Header().Set(k, v)
		}
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// Flush 当底层 ResponseWriter 支持 Flush 时调用
func (lrw *loggingResponseWriter) Flush() {
	if f, ok := lrw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func headersToCarrier(header http.Header) map[string]string {
	carrier := make(map[string]string)
	for k, v := range header {
		if len(v) > 0 {
			carrier[k] = v[0]
		}
	}
	return carrier
}
