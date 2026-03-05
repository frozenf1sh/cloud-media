package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	pb "github.com/frozenf1sh/cloud-media/proto/gen/api/v1"
	pbconnect "github.com/frozenf1sh/cloud-media/proto/gen/api/v1/v1connect"
)

const (
	apiServerAddr = "http://localhost:8080"
	pollInterval  = 2 * time.Second
)

// TestConfig 测试配置
type TestConfig struct {
	videoPath    string
	taskID       string
	apiAddr      string
	configPath   string
	templatePath string
	minioClient  *minio.Client
	videoClient  pbconnect.VideoServiceClient
	outputHTML   string
}

func main() {
	// 解析命令行参数
	videoPath := flag.String("video", "", "Path to video file (required)")
	taskID := flag.String("task-id", "", "Task ID (optional, auto-generated if not provided)")
	apiAddr := flag.String("api-addr", apiServerAddr, "API server address")
	configPath := flag.String("config", "", "Path to config.yaml")
	templatePath := flag.String("template", "", "Path to HTML template file (default: ./test/e2e/template.html)")
	outputHTML := flag.String("output", "test_result.html", "Output HTML file path")
	flag.Parse()

	if *videoPath == "" {
		flag.Usage()
		fmt.Println("\nExample: go run ./test/e2e -video ./test_video.mp4")
		os.Exit(1)
	}

	// 设置默认模板路径
	if *templatePath == "" {
		// 尝试找到模板文件
		candidates := []string{
			"./test/e2e/template.html",
			"./template.html",
			"./e2e/template.html",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				*templatePath = c
				break
			}
		}
		if *templatePath == "" {
			log_error("Template file not found, please specify with -template")
			os.Exit(1)
		}
	}

	// 初始化日志
	logger.InitSimple("debug")
	ctx := context.Background()
	traceID := uuid.New().String()
	ctx = logger.WithTraceID(ctx, traceID)

	log := slog.With("trace_id", traceID)
	log.Info("Starting end-to-end test",
		"video_path", *videoPath,
		"api_addr", *apiAddr,
		"template", *templatePath)

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Warn("Failed to load config, using defaults", "error", err)
		cfg = config.Default()
	}

	// 初始化测试配置
	testCfg := &TestConfig{
		videoPath:    *videoPath,
		taskID:       *taskID,
		apiAddr:      *apiAddr,
		configPath:   *configPath,
		templatePath: *templatePath,
		outputHTML:   *outputHTML,
	}

	// 运行测试
	result, err := runE2ETest(ctx, log, testCfg, cfg)
	if err != nil {
		log.Error("E2E test failed", "error", err)
		// 即使失败也尝试生成 HTML
		if result == nil {
			result = &TestResult{Error: err.Error()}
		}
	}

	// 生成 HTML 报告
	if err := generateHTMLReport(result, testCfg.templatePath, testCfg.outputHTML); err != nil {
		log.Error("Failed to generate HTML report", "error", err)
	}

	log.Info("Test completed", "html_output", testCfg.outputHTML)
}

// TestResult 测试结果
type TestResult struct {
	TaskID        string        `json:"task_id"`
	VideoPath     string        `json:"video_path"`
	Status        string        `json:"status"`
	Progress      int           `json:"progress"`
	SourceBucket  string        `json:"source_bucket"`
	SourceKey     string        `json:"source_key"`
	ErrorMessage  string        `json:"error_message,omitempty"`
	PlaylistURL   string        `json:"playlist_url,omitempty"`
	ThumbnailURL  string        `json:"thumbnail_url,omitempty"`
	Duration      time.Duration `json:"duration"`
	StartedAt     time.Time     `json:"started_at"`
	CompletedAt   time.Time     `json:"completed_at,omitempty"`
	Error         string        `json:"error,omitempty"`
	StorageEndpoint string      `json:"storage_endpoint"`
}

// runE2ETest 运行端到端测试
func runE2ETest(ctx context.Context, log *slog.Logger, testCfg *TestConfig, cfg *config.Config) (*TestResult, error) {
	startTime := time.Now()
	result := &TestResult{
		VideoPath:       testCfg.videoPath,
		StartedAt:       startTime,
		StorageEndpoint: cfg.ObjectStorage.ExternalEndpoint,
	}

	// 1. 验证视频文件存在
	log.Info("Step 1: Checking video file")
	if _, err := os.Stat(testCfg.videoPath); err != nil {
		result.Error = fmt.Sprintf("video file not found: %v", err)
		return result, fmt.Errorf("video file not found: %w", err)
	}
	log.Info("Video file found", "path", testCfg.videoPath)

	// 2. 初始化 MinIO 客户端
	log.Info("Step 2: Initializing MinIO client")
	minioClient, err := initMinIOClient(cfg)
	if err != nil {
		result.Error = fmt.Sprintf("failed to init MinIO: %v", err)
		return result, fmt.Errorf("failed to init MinIO: %w", err)
	}
	testCfg.minioClient = minioClient
	log.Info("MinIO client initialized", "endpoint", cfg.ObjectStorage.InternalEndpoint)

	// 3. 生成或使用任务 ID
	if testCfg.taskID == "" {
		testCfg.taskID = uuid.New().String()
	}
	result.TaskID = testCfg.taskID
	log.Info("Using task ID", "task_id", testCfg.taskID)

	// 4. 上传视频到 MinIO
	log.Info("Step 3: Uploading video to MinIO")
	sourceBucket := "media-input"
	sourceKey := fmt.Sprintf("uploads/%s/%s", testCfg.taskID, filepath.Base(testCfg.videoPath))
	result.SourceBucket = sourceBucket
	result.SourceKey = sourceKey

	if err := uploadToMinIO(ctx, log, minioClient, sourceBucket, sourceKey, testCfg.videoPath, cfg); err != nil {
		result.Error = fmt.Sprintf("failed to upload video: %v", err)
		return result, fmt.Errorf("failed to upload video: %w", err)
	}
	log.Info("Video uploaded", "bucket", sourceBucket, "key", sourceKey)

	// 5. 初始化 API 客户端
	log.Info("Step 4: Initializing API client")
	testCfg.videoClient = pbconnect.NewVideoServiceClient(
		http.DefaultClient,
		testCfg.apiAddr,
	)
	log.Info("API client initialized", "address", testCfg.apiAddr)

	// 6. 提交转码任务
	log.Info("Step 5: Submitting transcoding task")
	if err := submitTask(ctx, log, testCfg, sourceBucket, sourceKey); err != nil {
		result.Error = fmt.Sprintf("failed to submit task: %v", err)
		return result, fmt.Errorf("failed to submit task: %w", err)
	}
	log.Info("Task submitted successfully")

	// 7. 轮询任务状态
	log.Info("Step 6: Polling task status")
	finalStatus, err := pollTaskStatus(ctx, log, testCfg, result)
	if err != nil {
		result.Error = fmt.Sprintf("task failed: %v", err)
		return result, fmt.Errorf("task failed: %w", err)
	}

	result.Status = finalStatus
	result.CompletedAt = time.Now()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)

	// 8. 生成播放 URL
	if finalStatus == "success" {
		log.Info("Step 7: Generating playback URLs")
		result.PlaylistURL = fmt.Sprintf("http://%s/media-output/%s/master.m3u8",
			cfg.ObjectStorage.ExternalEndpoint, testCfg.taskID)
		result.ThumbnailURL = fmt.Sprintf("http://%s/media-output/%s/thumbnail.jpg",
			cfg.ObjectStorage.ExternalEndpoint, testCfg.taskID)
		log.Info("Playback URLs generated",
			"playlist", result.PlaylistURL,
			"thumbnail", result.ThumbnailURL)
	}

	log.Info("E2E test completed successfully",
		"duration", result.Duration,
		"status", finalStatus)

	return result, nil
}

// initMinIOClient 初始化 MinIO/S3 兼容客户端
func initMinIOClient(cfg *config.Config) (*minio.Client, error) {
	region := cfg.ObjectStorage.Region
	if region == "" {
		region = "us-east-1"
	}
	return minio.New(cfg.ObjectStorage.InternalEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.ObjectStorage.AccessKeyID, cfg.ObjectStorage.SecretAccessKey, ""),
		Secure: cfg.ObjectStorage.InternalUseSSL,
		Region: region,
	})
}

// uploadToMinIO 上传文件到 MinIO/S3
func uploadToMinIO(ctx context.Context, log *slog.Logger, client *minio.Client, bucket, key, filePath string, _ *config.Config) error {
	// 确保 bucket 存在
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket: %w", err)
	}
	if !exists {
		log.Info("Creating bucket", "bucket", bucket)
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// 检测 Content-Type
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	log.Info("Uploading file",
		"bucket", bucket,
		"key", key,
		"size", fileInfo.Size(),
		"content_type", contentType)

	// 上传文件
	_, err = client.PutObject(ctx, bucket, key, file, fileInfo.Size(), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("failed to put object: %w", err)
	}

	return nil
}

// submitTask 提交转码任务
func submitTask(ctx context.Context, log *slog.Logger, testCfg *TestConfig, bucket, key string) error {
	req := connect.NewRequest(&pb.SubmitTaskRequest{
		TaskId:       testCfg.taskID, // 可选，如果为空则服务端生成
		SourceBucket: bucket,
		SourceKey:    key,
	})

	resp, err := testCfg.videoClient.SubmitTask(ctx, req)
	if err != nil {
		return fmt.Errorf("SubmitTask failed: %w", err)
	}

	// 如果服务端返回了新的 taskID，更新我们的 taskID
	if resp.Msg.TaskId != "" && resp.Msg.TaskId != testCfg.taskID {
		log.Info("Server generated task ID",
			"client_task_id", testCfg.taskID,
			"server_task_id", resp.Msg.TaskId)
		testCfg.taskID = resp.Msg.TaskId
	}

	log.Info("SubmitTask response",
		"task_id", resp.Msg.TaskId,
		"status", resp.Msg.Status,
		"message", resp.Msg.Message)

	return nil
}

// pollTaskStatus 轮询任务状态
func pollTaskStatus(ctx context.Context, log *slog.Logger, testCfg *TestConfig, result *TestResult) (string, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			status, progress, errMsg, err := getTaskStatus(ctx, testCfg)
			if err != nil {
				log.Warn("Failed to get task status", "error", err)
				continue
			}

			result.Status = status
			result.Progress = progress
			result.ErrorMessage = errMsg

			log.Info("Task status update",
				"status", status,
				"progress", progress,
				"error", errMsg)

			// 检查终端状态
			switch status {
			case "success":
				return status, nil
			case "failed":
				return status, fmt.Errorf("task failed: %s", errMsg)
			case "cancelled":
				return status, fmt.Errorf("task was cancelled")
			}
		}
	}
}

// getTaskStatus 获取任务状态
func getTaskStatus(ctx context.Context, testCfg *TestConfig) (string, int, string, error) {
	req := connect.NewRequest(&pb.GetTaskStatusRequest{
		TaskId: testCfg.taskID,
	})

	resp, err := testCfg.videoClient.GetTaskStatus(ctx, req)
	if err != nil {
		return "", 0, "", err
	}

	return resp.Msg.Status, int(resp.Msg.Progress), resp.Msg.ErrorMessage, nil
}

// generateHTMLReport 生成 HTML 报告
func generateHTMLReport(result *TestResult, templatePath, outputPath string) error {
	// 读取模板文件
	tmplBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template file: %w", err)
	}

	// 解析模板
	tmpl, err := template.New("report").Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// 创建输出文件
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	// 执行模板
	if err := tmpl.Execute(file, result); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

// log_error 打印错误日志
func log_error(msg string) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", msg)
}
