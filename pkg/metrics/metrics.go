package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// API 指标
	apiRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_requests_total",
			Help: "Total number of API requests",
		},
		[]string{"method", "path", "status"},
	)

	apiRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "api_request_duration_seconds",
			Help:    "API request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// 任务指标
	tasksTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tasks_total",
			Help: "Total number of tasks processed",
		},
		[]string{"status"},
	)

	taskDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "task_duration_seconds",
			Help:    "Task processing duration in seconds",
			Buckets: []float64{10, 30, 60, 120, 300, 600, 1800, 3600},
		},
		[]string{"status"},
	)

	tasksInProgress = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tasks_in_progress",
			Help: "Number of tasks currently being processed",
		},
	)

	// 队列指标
	queueSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "queue_size",
			Help: "Number of tasks waiting in queue",
		},
	)

	// 转码指标
	transcodedBytes = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "transcoded_bytes_total",
			Help: "Total bytes transcoded",
		},
	)

	transcodedVideos = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "transcoded_videos_total",
			Help: "Total number of videos transcoded",
		},
		[]string{"quality"},
	)
)

// RecordAPIRequest 记录 API 请求
func RecordAPIRequest(method, path string, status int, duration time.Duration) {
	statusStr := strconv.Itoa(status)
	apiRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	apiRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// RecordTaskStarted 记录任务开始
func RecordTaskStarted() {
	tasksInProgress.Inc()
}

// RecordTaskCompleted 记录任务完成
func RecordTaskCompleted(status string, duration time.Duration) {
	tasksInProgress.Dec()
	tasksTotal.WithLabelValues(status).Inc()
	taskDuration.WithLabelValues(status).Observe(duration.Seconds())
}

// SetQueueSize 设置队列大小
func SetQueueSize(size int) {
	queueSize.Set(float64(size))
}

// RecordTranscodedBytes 记录转码字节数
func RecordTranscodedBytes(bytes int64) {
	transcodedBytes.Add(float64(bytes))
}

// RecordTranscodedVideo 记录转码视频
func RecordTranscodedVideo(quality string) {
	transcodedVideos.WithLabelValues(quality).Inc()
}

// HTTPHandler 返回 Prometheus HTTP 处理器
func HTTPHandler() http.Handler {
	return promhttp.Handler()
}

// Registry 返回 Prometheus 注册表
func Registry() *prometheus.Registry {
	return prometheus.DefaultRegisterer.(*prometheus.Registry)
}
