// Package health 提供健康检查功能，支持 Kubernetes liveness/readiness 探针。
//
// 实现 RFC draft-inadarei-api-health-check-06 规范
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Status 健康状态
type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Check 健康检查结果
type Check struct {
	Status    Status            `json:"status"`
	Output    string            `json:"output,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	ServiceID string            `json:"service_id,omitempty"`
	Version   string            `json:"version,omitempty"`
	Checks    map[string]Check  `json:"checks,omitempty"`
}

// Checker 健康检查器接口
type Checker interface {
	Name() string
	Check(ctx context.Context) Check
}

// CheckerFunc 健康检查函数类型
type CheckerFunc func(ctx context.Context) Check

// Health 健康检查管理器
type Health struct {
	mu       sync.RWMutex
	checkers map[string]Checker
	status   Status
	serviceID string
	version   string
}

// New 创建健康检查管理器
func New(serviceID, version string) *Health {
	return &Health{
		checkers:  make(map[string]Checker),
		status:    StatusPass,
		serviceID: serviceID,
		version:   version,
	}
}

// Register 注册健康检查器
func (h *Health) Register(checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[checker.Name()] = checker
}

// RegisterFunc 注册健康检查函数
func (h *Health) RegisterFunc(name string, fn CheckerFunc) {
	h.Register(&funcChecker{name: name, fn: fn})
}

// Check 执行所有健康检查
func (h *Health) Check(ctx context.Context) Check {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := Check{
		Status:    StatusPass,
		Timestamp: time.Now().UTC(),
		ServiceID: h.serviceID,
		Version:   h.version,
		Checks:    make(map[string]Check),
	}

	for name, checker := range h.checkers {
		check := checker.Check(ctx)
		result.Checks[name] = check

		if check.Status == StatusFail {
			result.Status = StatusFail
		} else if check.Status == StatusWarn && result.Status == StatusPass {
			result.Status = StatusWarn
		}
	}

	return result
}

// HTTPHandler 返回 HTTP 处理器
func (h *Health) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		check := h.Check(r.Context())

		w.Header().Set("Content-Type", "application/health+json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

		switch check.Status {
		case StatusPass:
			w.WriteHeader(http.StatusOK)
		case StatusWarn:
			w.WriteHeader(http.StatusOK)
		case StatusFail:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		_ = json.NewEncoder(w).Encode(check)
	})
}

// LivenessHandler 存活探针 - 简单返回 200
func LivenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("OK"))
	})
}

// funcChecker 函数包装器
type funcChecker struct {
	name string
	fn   CheckerFunc
}

func (fc *funcChecker) Name() string {
	return fc.name
}

func (fc *funcChecker) Check(ctx context.Context) Check {
	return fc.fn(ctx)
}

// SimpleCheck 创建简单的健康检查
func SimpleCheck(checkFn func(ctx context.Context) error) CheckerFunc {
	return func(ctx context.Context) Check {
		start := time.Now()
		err := checkFn(ctx)
		duration := time.Since(start)

		result := Check{
			Status:    StatusPass,
			Timestamp: time.Now().UTC(),
		}

		if err != nil {
			result.Status = StatusFail
			result.Output = err.Error()
		} else if duration > 5*time.Second {
			result.Status = StatusWarn
			result.Output = "slow response: " + duration.String()
		}

		return result
	}
}
