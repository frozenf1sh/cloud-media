package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/frozenf1sh/cloud-media/pkg/logger"
	pb "github.com/frozenf1sh/cloud-media/proto/gen/api/v1"
	pbconnect "github.com/frozenf1sh/cloud-media/proto/gen/api/v1/v1connect"
)

const (
	defaultApiServerAddr = "http://media-api.frozenf1sh.loc/"
	pollInterval         = 2 * time.Second
)

// Task status constants
const (
	TaskStatusPending    = "pending"
	TaskStatusQueued     = "queued"
	TaskStatusProcessing = "processing"
	TaskStatusSuccess    = "success"
	TaskStatusFailed     = "failed"
	TaskStatusCancelled  = "cancelled"
)

// TaskResult holds the result of a single task
type TaskResult struct {
	TaskID          string
	SubmitTime      time.Time
	StartTime       time.Time
	EndTime         time.Time
	Status          string
	Error           string
	QueuedDuration  time.Duration
	ProcessDuration time.Duration
	TotalDuration   time.Duration
}

// Stats holds aggregate statistics
type Stats struct {
	TotalTasks       int
	Successful       int
	Failed           int
	TotalDuration    time.Duration
	SubmitDuration   time.Duration
	QueuedDurations  []time.Duration
	ProcessDurations []time.Duration
	TotalDurations   []time.Duration
	Throughput       float64 // tasks per second
}

func main() {
	videoPath := flag.String("video", "", "Path to video file (required)")
	count := flag.Int("count", 10, "Number of tasks to submit")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent workers")
	apiAddr := flag.String("api-addr", defaultApiServerAddr, "API server address")
	interval := flag.Duration("interval", 0, "Interval between task submissions (e.g., 1s, 500ms)")
	reuseUpload := flag.Bool("reuse-upload", false, "Upload once and reuse the same file for all tasks")
	flag.Parse()

	if *videoPath == "" {
		flag.Usage()
		fmt.Println("\nExample: go run ./test/loadtest -video ./test_video.mp4 -count 20 -concurrency 5")
		fmt.Println("\nFor k8s environment:")
		fmt.Println("  go run ./test/loadtest -video ./test/test.mp4 -api-addr http://media-api.frozenf1sh.loc/ -count 50 -concurrency 10")
		fmt.Println("\nWith upload reuse (faster):")
		fmt.Println("  go run ./test/loadtest -video ./test/test.mp4 -count 50 -reuse-upload")
		os.Exit(1)
	}

	logger.InitSimple("info")
	ctx := context.Background()

	log := slog.With(
		"video_path", *videoPath,
		"count", *count,
		"concurrency", *concurrency,
		"api_addr", *apiAddr,
		"reuse_upload", *reuseUpload,
	)

	log.Info("Starting load test")

	fileInfo, err := os.Stat(*videoPath)
	if err != nil {
		log.Error("Video file not found", "error", err)
		os.Exit(1)
	}
	log.Info("Video file found", "size", fileInfo.Size())

	videoClient := pbconnect.NewVideoServiceClient(http.DefaultClient, *apiAddr)
	log.Info("API client initialized")

	// Phase 1: Submit all tasks
	log.Info("Phase 1: Submitting tasks")
	submitStart := time.Now()

	var sourceBucket, sourceKey string
	if *reuseUpload {
		log.Info("Reuse-upload enabled: uploading file once")
		taskID := uuid.New().String()
		uploadResp, err := getUploadURL(ctx, log, videoClient, taskID, filepath.Base(*videoPath), fileInfo.Size())
		if err != nil {
			log.Error("Failed to get upload URL", "error", err)
			os.Exit(1)
		}
		if err := uploadViaPresignedURL(ctx, log, *videoPath, uploadResp.UploadUrl); err != nil {
			log.Error("Failed to upload video", "error", err)
			os.Exit(1)
		}
		sourceBucket = uploadResp.SourceBucket
		sourceKey = uploadResp.SourceKey
		log.Info("File uploaded successfully, will reuse for all tasks",
			"bucket", sourceBucket, "key", sourceKey)
	}

	taskChan := make(chan int, *count)
	for i := 0; i < *count; i++ {
		taskChan <- i
	}
	close(taskChan)

	var taskResults sync.Map // map[string]*TaskResult
	var submitSuccessCount, submitFailCount int32
	var wg sync.WaitGroup

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			workerLog := log.With("worker_id", workerID)

			for range taskChan {
				taskSubmitTime := time.Now()
				taskID := uuid.New().String()
				taskLog := workerLog.With("task_id", taskID)

				var err error
				if *reuseUpload {
					err = submitTaskOnly(ctx, taskLog, videoClient, taskID, sourceBucket, sourceKey)
				} else {
					err = uploadAndSubmitTask(ctx, taskLog, videoClient, taskID, *videoPath)
				}

				result := &TaskResult{
					TaskID:     taskID,
					SubmitTime: taskSubmitTime,
				}

				if err != nil {
					atomic.AddInt32(&submitFailCount, 1)
					result.Status = TaskStatusFailed
					result.Error = err.Error()
					taskLog.Warn("Task submission failed", "error", err)
				} else {
					atomic.AddInt32(&submitSuccessCount, 1)
					result.Status = TaskStatusQueued
					taskLog.Debug("Task submitted successfully")
				}

				taskResults.Store(taskID, result)

				if *interval > 0 {
					time.Sleep(*interval)
				}
			}
		}(i)
	}

	wg.Wait()
	submitDuration := time.Since(submitStart)

	log.Info("Phase 1 complete: All tasks submitted",
		"submitted", submitSuccessCount,
		"failed_submit", submitFailCount,
		"duration", submitDuration)

	if submitSuccessCount == 0 {
		log.Error("No tasks were successfully submitted")
		os.Exit(1)
	}

	// Phase 2: Poll for task completion
	log.Info("Phase 2: Waiting for tasks to complete")
	pollStart := time.Now()

	pollTasks(ctx, log, videoClient, &taskResults, int(submitSuccessCount))
	totalDuration := time.Since(submitStart)
	pollDuration := time.Since(pollStart)

	log.Info("Phase 2 complete: All tasks finished", "duration", pollDuration)

	// Generate statistics
	stats := calculateStats(&taskResults, submitDuration, *count)

	// Print summary
	printSummary(log, stats, &taskResults, submitDuration, pollDuration, totalDuration)
}

// pollTasks polls all tasks until they reach a terminal state
func pollTasks(ctx context.Context, log *slog.Logger, client pbconnect.VideoServiceClient, taskResults *sync.Map, expectedCount int) {
	completedCount := 0
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for completedCount < expectedCount {
		<-ticker.C

		currentCompleted := 0
		inProgress := 0
		queued := 0

		taskResults.Range(func(key, value interface{}) bool {
			taskID := key.(string)
			result := value.(*TaskResult)

			// Skip if already in terminal state
			if isTerminalState(result.Status) {
				currentCompleted++
				return true
			}

			// Poll current status
			statusResp, err := getTaskStatus(ctx, client, taskID)
			if err != nil {
				log.Debug("Failed to get task status", "task_id", taskID, "error", err)
				return true
			}

			oldStatus := result.Status
			result.Status = statusResp.Status

			// Record start time when moving to processing
			if oldStatus != TaskStatusProcessing &&
				result.Status == TaskStatusProcessing {
				result.StartTime = time.Now()
				log.Debug("Task started processing", "task_id", taskID)
			}

			// Record end time when reaching terminal state
			if isTerminalState(result.Status) {
				result.EndTime = time.Now()
				if !result.StartTime.IsZero() {
					result.ProcessDuration = result.EndTime.Sub(result.StartTime)
				}
				if !result.StartTime.IsZero() {
					result.QueuedDuration = result.StartTime.Sub(result.SubmitTime)
				}
				result.TotalDuration = result.EndTime.Sub(result.SubmitTime)

				if result.Status == TaskStatusFailed && statusResp.ErrorMessage != "" {
					result.Error = statusResp.ErrorMessage
				}

				currentCompleted++
				log.Info("Task completed",
					"task_id", taskID,
					"status", result.Status,
					"total_duration", result.TotalDuration)
			} else if result.Status == TaskStatusProcessing {
				inProgress++
			} else {
				queued++
			}

			return true
		})

		completedCount = currentCompleted

		if completedCount < expectedCount {
			log.Info("Progress update",
				"completed", completedCount,
				"in_progress", inProgress,
				"queued", queued,
				"remaining", expectedCount-completedCount)
		}
	}
}

func isTerminalState(status string) bool {
	return status == TaskStatusSuccess ||
		status == TaskStatusFailed ||
		status == TaskStatusCancelled
}

func getTaskStatus(ctx context.Context, client pbconnect.VideoServiceClient, taskID string) (*pb.GetTaskStatusResponse, error) {
	req := connect.NewRequest(&pb.GetTaskStatusRequest{
		TaskId: taskID,
	})

	resp, err := client.GetTaskStatus(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Msg, nil
}

func calculateStats(taskResults *sync.Map, submitDuration time.Duration, totalCount int) *Stats {
	stats := &Stats{
		TotalTasks: totalCount,
	}

	var minSubmitTime time.Time
	var maxEndTime time.Time

	taskResults.Range(func(key, value interface{}) bool {
		result := value.(*TaskResult)

		if result.Status == TaskStatusSuccess {
			stats.Successful++
		} else if result.Status == TaskStatusFailed || result.Status == TaskStatusCancelled {
			stats.Failed++
		}

		if result.QueuedDuration > 0 {
			stats.QueuedDurations = append(stats.QueuedDurations, result.QueuedDuration)
		}
		if result.ProcessDuration > 0 {
			stats.ProcessDurations = append(stats.ProcessDurations, result.ProcessDuration)
		}
		if result.TotalDuration > 0 {
			stats.TotalDurations = append(stats.TotalDurations, result.TotalDuration)
		}

		// Track min submit time and max end time for throughput calculation
		if result.Status == TaskStatusSuccess {
			if minSubmitTime.IsZero() || result.SubmitTime.Before(minSubmitTime) {
				minSubmitTime = result.SubmitTime
			}
			if !result.EndTime.IsZero() && (maxEndTime.IsZero() || result.EndTime.After(maxEndTime)) {
				maxEndTime = result.EndTime
			}
		}

		return true
	})

	if stats.Successful > 0 && !maxEndTime.IsZero() && !minSubmitTime.IsZero() {
		stats.TotalDuration = maxEndTime.Sub(minSubmitTime)
		if stats.TotalDuration.Seconds() > 0 {
			stats.Throughput = float64(stats.Successful) / stats.TotalDuration.Seconds()
		}
	}

	return stats
}

func printSummary(log *slog.Logger, stats *Stats, taskResults *sync.Map, submitDuration, pollDuration, totalDuration time.Duration) {
	fmt.Printf("\n")
	fmt.Printf("==============================================\n")
	fmt.Printf("          LOAD TEST SUMMARY\n")
	fmt.Printf("==============================================\n\n")

	fmt.Printf("--- Submission Phase ---\n")
	fmt.Printf("Submit Duration:    %v\n", submitDuration)
	fmt.Printf("\n")

	fmt.Printf("--- Processing Phase ---\n")
	fmt.Printf("Poll Duration:      %v\n", pollDuration)
	fmt.Printf("\n")

	fmt.Printf("--- Overall Results ---\n")
	fmt.Printf("Total Tasks:        %d\n", stats.TotalTasks)
	fmt.Printf("Successful:         %d\n", stats.Successful)
	fmt.Printf("Failed:             %d\n", stats.Failed)
	if stats.TotalTasks > 0 {
		fmt.Printf("Success Rate:       %.2f%%\n", float64(stats.Successful)/float64(stats.TotalTasks)*100)
	}
	fmt.Printf("Total Wall Time:    %v\n", totalDuration)
	if stats.Throughput > 0 {
		fmt.Printf("Throughput:         %.2f tasks/sec\n", stats.Throughput)
	}
	fmt.Printf("\n")

	if len(stats.QueuedDurations) > 0 {
		fmt.Printf("--- Queue Time Statistics ---\n")
		printDurationStats(stats.QueuedDurations)
		fmt.Printf("\n")
	}

	if len(stats.ProcessDurations) > 0 {
		fmt.Printf("--- Processing Time Statistics ---\n")
		printDurationStats(stats.ProcessDurations)
		fmt.Printf("\n")
	}

	if len(stats.TotalDurations) > 0 {
		fmt.Printf("--- Total Time Statistics ---\n")
		printDurationStats(stats.TotalDurations)
		fmt.Printf("\n")
	}

	// Show failed tasks if any
	if stats.Failed > 0 {
		fmt.Printf("--- Failed Tasks ---\n")
		taskResults.Range(func(key, value interface{}) bool {
			result := value.(*TaskResult)
			if result.Status == TaskStatusFailed || result.Status == TaskStatusCancelled {
				fmt.Printf("  Task %s: %s\n", result.TaskID, result.Error)
			}
			return true
		})
		fmt.Printf("\n")
	}

	fmt.Printf("==============================================\n\n")

	log.Info("Load test completed",
		"total_tasks", stats.TotalTasks,
		"successful", stats.Successful,
		"failed", stats.Failed,
		"total_duration", totalDuration)
}

func printDurationStats(durations []time.Duration) {
	if len(durations) == 0 {
		return
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	sum := time.Duration(0)
	for _, d := range durations {
		sum += d
	}

	mean := sum / time.Duration(len(durations))
	p50 := durations[len(durations)*50/100]
	p95 := durations[len(durations)*95/100]
	p99 := durations[len(durations)*99/100]
	min := durations[0]
	max := durations[len(durations)-1]

	// Calculate standard deviation
	var varianceSum float64
	meanSec := mean.Seconds()
	for _, d := range durations {
		diff := d.Seconds() - meanSec
		varianceSum += diff * diff
	}
	stdDev := time.Duration(math.Sqrt(varianceSum/float64(len(durations))) * float64(time.Second))

	fmt.Printf("  Count:    %d\n", len(durations))
	fmt.Printf("  Min:      %v\n", min)
	fmt.Printf("  Max:      %v\n", max)
	fmt.Printf("  Mean:     %v\n", mean)
	fmt.Printf("  StdDev:   %v\n", stdDev)
	fmt.Printf("  P50:      %v\n", p50)
	fmt.Printf("  P95:      %v\n", p95)
	fmt.Printf("  P99:      %v\n", p99)
}

func uploadAndSubmitTask(ctx context.Context, log *slog.Logger, client pbconnect.VideoServiceClient, taskID, videoPath string) error {
	fileInfo, err := os.Stat(videoPath)
	if err != nil {
		return err
	}

	uploadResp, err := getUploadURL(ctx, log, client, taskID, filepath.Base(videoPath), fileInfo.Size())
	if err != nil {
		return fmt.Errorf("failed to get upload URL: %w", err)
	}

	if err := uploadViaPresignedURL(ctx, log, videoPath, uploadResp.UploadUrl); err != nil {
		return fmt.Errorf("failed to upload video: %w", err)
	}

	if err := submitTaskOnly(ctx, log, client, taskID, uploadResp.SourceBucket, uploadResp.SourceKey); err != nil {
		return err
	}

	return nil
}

func submitTaskOnly(ctx context.Context, log *slog.Logger, client pbconnect.VideoServiceClient, taskID, bucket, key string) error {
	req := connect.NewRequest(&pb.SubmitTaskRequest{
		TaskId:       taskID,
		SourceBucket: bucket,
		SourceKey:    key,
	})

	resp, err := client.SubmitTask(ctx, req)
	if err != nil {
		return fmt.Errorf("SubmitTask failed: %w", err)
	}

	log.Debug("Task submitted",
		"task_id", resp.Msg.TaskId,
		"status", resp.Msg.Status)

	return nil
}

func getUploadURL(ctx context.Context, log *slog.Logger, client pbconnect.VideoServiceClient, taskID, fileName string, fileSize int64) (*pb.GetUploadURLResponse, error) {
	req := connect.NewRequest(&pb.GetUploadURLRequest{
		TaskId:   taskID,
		FileName: fileName,
		FileSize: fileSize,
	})

	resp, err := client.GetUploadURL(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetUploadURL failed: %w", err)
	}

	log.Debug("Got upload URL",
		"task_id", resp.Msg.TaskId,
		"source_bucket", resp.Msg.SourceBucket,
		"source_key", resp.Msg.SourceKey)

	return resp.Msg, nil
}

func uploadViaPresignedURL(ctx context.Context, log *slog.Logger, filePath, uploadURL string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", uploadURL, file)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.ContentLength = fileInfo.Size()

	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	httpReq.Header.Set("Content-Type", contentType)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upload failed with status code: %d", resp.StatusCode)
	}

	return nil
}
