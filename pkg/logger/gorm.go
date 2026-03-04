package logger

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm/logger"
)

// GormLogger 是 GORM 的日志适配器
type GormLogger struct {
	SlowThreshold         time.Duration
	SkipErrRecordNotFound bool
}

// NewGormLogger 创建 GORM 日志适配器
func NewGormLogger() *GormLogger {
	return &GormLogger{
		SlowThreshold:         200 * time.Millisecond,
		SkipErrRecordNotFound: true,
	}
}

// LogMode 实现 logger.Interface
func (l *GormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return l
}

// Info 实现 logger.Interface
func (l *GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	InfoContext(ctx, fmt.Sprintf(msg, data...))
}

// Warn 实现 logger.Interface
func (l *GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	WarnContext(ctx, fmt.Sprintf(msg, data...))
}

// Error 实现 logger.Interface
func (l *GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	ErrorContext(ctx, fmt.Sprintf(msg, data...))
}

// Trace 实现 logger.Interface
func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()

	attrs := []any{
		String("duration", fmt.Sprintf("%.3fms", float64(elapsed.Nanoseconds())/1e6)),
		String("sql", sql),
		Int64("rows", rows),
	}

	if err != nil {
		if !(l.SkipErrRecordNotFound && err.Error() == "record not found") {
			attrs = append(attrs, Err(err))
			ErrorContext(ctx, "Database query failed", attrs...)
		} else {
			DebugContext(ctx, "Database query", attrs...)
		}
	} else if elapsed > l.SlowThreshold {
		attrs = append(attrs, String("slow", "true"))
		WarnContext(ctx, "Slow database query", attrs...)
	} else {
		DebugContext(ctx, "Database query", attrs...)
	}
}
