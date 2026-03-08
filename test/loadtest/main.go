package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
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
)

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

	startTime := time.Now()
	var successCount, failCount int32
	var wg sync.WaitGroup

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

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			workerLog := log.With("worker_id", workerID)

			for range taskChan {
				taskStartTime := time.Now()
				taskID := uuid.New().String()
				taskLog := workerLog.With("task_id", taskID)

				var err error
				if *reuseUpload {
					err = submitTaskOnly(ctx, taskLog, videoClient, taskID, sourceBucket, sourceKey)
				} else {
					err = uploadAndSubmitTask(ctx, taskLog, videoClient, taskID, *videoPath)
				}

				if err != nil {
					atomic.AddInt32(&failCount, 1)
					taskLog.Warn("Task failed", "error", err, "duration", time.Since(taskStartTime))
				} else {
					atomic.AddInt32(&successCount, 1)
					taskLog.Info("Task submitted successfully", "duration", time.Since(taskStartTime))
				}

				if *interval > 0 {
					time.Sleep(*interval)
				}
			}
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(startTime)

	log.Info("Load test completed",
		"total_tasks", *count,
		"successful_tasks", successCount,
		"failed_tasks", failCount,
		"total_duration", totalDuration)

	fmt.Printf("\n=== Load Test Summary ===\n")
	fmt.Printf("Total Tasks:     %d\n", *count)
	fmt.Printf("Successful:      %d\n", successCount)
	fmt.Printf("Failed:          %d\n", failCount)
	fmt.Printf("Total Duration:  %v\n", totalDuration)
	if *count > 0 {
		fmt.Printf("Avg Task Time:   %v\n", totalDuration/time.Duration(*count))
	}
	fmt.Printf("========================\n\n")
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

