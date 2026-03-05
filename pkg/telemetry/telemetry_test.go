package telemetry

import (
	"context"
	"errors"
	"testing"
)

func TestStartSpan(t *testing.T) {
	ctx := context.Background()

	// 即使没有配置 tracer，也不应 panic
	ctx, span := StartSpan(ctx, "test-span",
		String("key1", "value1"),
		Int("key2", 123),
	)
	defer span.End()

	if ctx == nil {
		t.Error("StartSpan returned nil context")
	}
}

func TestStartSpanWithAttributes(t *testing.T) {
	ctx := context.Background()

	// 即使没有配置 tracer，也不应 panic
	ctx, span := StartSpan(ctx, "test-span",
		String("key1", "value1"),
		Int("key2", 123),
		Int64("key3", 456),
	)
	defer span.End()

	if ctx == nil {
		t.Error("StartSpan returned nil context")
	}
}

func TestTraceIDFromContext(t *testing.T) {
	ctx := context.Background()

	// 没有 span 时应该返回空字符串
	traceID := TraceIDFromContext(ctx)
	if traceID != "" {
		t.Errorf("Expected empty trace ID, got '%s'", traceID)
	}

	// 有 span 时应该返回 trace ID（或空，但不应 panic）
	ctx, span := StartSpan(ctx, "test-span")
	defer span.End()

	_ = TraceIDFromContext(ctx)
	// 不应 panic
}

func TestRecordError(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-span")
	defer span.End()

	testErr := errors.New("test error")
	// 不应 panic
	RecordError(ctx, testErr, String("error.type", "test"))
}

func TestAttributeHelpers(t *testing.T) {
	attr1 := String("key1", "value1")
	if attr1.Key != "key1" {
		t.Errorf("String attribute key not correct")
	}

	attr2 := Int("key2", 123)
	if attr2.Key != "key2" {
		t.Errorf("Int attribute key not correct")
	}

	attr3 := Int64("key3", 456)
	if attr3.Key != "key3" {
		t.Errorf("Int64 attribute key not correct")
	}
}

func TestWithTraceID(t *testing.T) {
	ctx := context.Background()

	// 测试无效的 trace ID，不应 panic
	ctx2 := WithTraceID(ctx, "invalid-trace-id")
	_ = ctx2

	// 测试有效的 trace ID
	validTraceID := "0123456789abcdef0123456789abcdef"
	ctx3 := WithTraceID(ctx, validTraceID)
	if ctx3 == nil {
		t.Errorf("WithTraceID returned nil context")
	}
}

func TestExtractAndInjectCarrier(t *testing.T) {
	ctx := context.Background()

	// 测试空 carrier，不应 panic
	ctx2 := ExtractFromCarrier(ctx, map[string]string{})
	if ctx2 == nil {
		t.Errorf("ExtractFromCarrier returned nil context")
	}

	// 测试注入，不应 panic
	carrier := make(map[string]string)
	InjectToCarrier(ctx, carrier)
}

func TestAddEvent(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-span")
	defer span.End()

	// 不应 panic
	AddEvent(ctx, "test-event", String("key", "value"))
}

func TestSetAttributes(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-span")
	defer span.End()

	// 不应 panic
	SetAttributes(ctx, String("key", "value"))
}

func TestSetSpanStatusOK(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-span")
	defer span.End()

	// 不应 panic
	SetSpanStatusOK(ctx)
}

func TestSetSpanStatusError(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-span")
	defer span.End()

	// 不应 panic
	SetSpanStatusError(ctx, "something went wrong")
}
