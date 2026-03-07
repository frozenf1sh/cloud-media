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

	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	pb "github.com/frozenf1sh/cloud-media/proto/gen/api/v1"
	pbconnect "github.com/frozenf1sh/cloud-media/proto/gen/api/v1/v1connect"
)

const (
	defaultApiServerAddr = "http://media-api.frozenf1sh.loc/"
	pollInterval         = 2 * time.Second
)

// TestConfig 测试配置
type TestConfig struct {
	videoPath    string
	taskID       string
	apiAddr      string
	configPath   string
	templatePath string
	videoClient  pbconnect.VideoServiceClient
	outputHTML   string
}

func main() {
	// 解析命令行参数
	videoPath := flag.String("video", "", "Path to video file (required)")
	taskID := flag.String("task-id", "", "Task ID (optional, auto-generated if not provided)")
	apiAddr := flag.String("api-addr", defaultApiServerAddr, "API server address")
	configPath := flag.String("config", "", "Path to config.yaml (optional)")
	templatePath := flag.String("template", "", "Path to HTML template file (default: ./test/e2e/template.html)")
	outputHTML := flag.String("output", "test_result.html", "Output HTML file path")
	flag.Parse()

	if *videoPath == "" {
		flag.Usage()
		fmt.Println("\nExample: go run ./test/e2e -video ./test_video.mp4")
		fmt.Println("\nFor k8s environment:")
		fmt.Println("  go run ./test/e2e -video ./test/test.mp4 -api-addr http://media-api.frozenf1sh.loc/")
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

	// 加载配置（可选，用于备用）
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Warn("Failed to load config, not needed for API-only mode", "error", err)
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
	TaskID          string        `json:"task_id"`
	VideoPath       string        `json:"video_path"`
	Status          string        `json:"status"`
	Progress        int           `json:"progress"`
	SourceBucket    string        `json:"source_bucket"`
	SourceKey       string        `json:"source_key"`
	ErrorMessage    string        `json:"error_message,omitempty"`
	PlaylistURL     string        `json:"playlist_url,omitempty"`
	ThumbnailURL    string        `json:"thumbnail_url,omitempty"`
	Duration        time.Duration `json:"duration"`
	StartedAt       time.Time     `json:"started_at"`
	CompletedAt     time.Time     `json:"completed_at,omitempty"`
	Error           string        `json:"error,omitempty"`
	StorageEndpoint string        `json:"storage_endpoint"`
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
	fileInfo, err := os.Stat(testCfg.videoPath)
	if err != nil {
		result.Error = fmt.Sprintf("video file not found: %v", err)
		return result, fmt.Errorf("video file not found: %w", err)
	}
	log.Info("Video file found", "path", testCfg.videoPath, "size", fileInfo.Size())

	// 2. 初始化 API 客户端
	log.Info("Step 2: Initializing API client")
	testCfg.videoClient = pbconnect.NewVideoServiceClient(
		http.DefaultClient,
		testCfg.apiAddr,
	)
	log.Info("API client initialized", "address", testCfg.apiAddr)

	// 3. 获取上传预签名 URL
	log.Info("Step 3: Getting upload URL from API")
	fileName := filepath.Base(testCfg.videoPath)
	uploadResp, err := getUploadURL(ctx, log, testCfg, fileName, fileInfo.Size())
	if err != nil {
		result.Error = fmt.Sprintf("failed to get upload URL: %v", err)
		return result, fmt.Errorf("failed to get upload URL: %w", err)
	}
	testCfg.taskID = uploadResp.TaskId
	result.TaskID = testCfg.taskID
	result.SourceBucket = uploadResp.SourceBucket
	result.SourceKey = uploadResp.SourceKey
	log.Info("Got upload URL",
		"task_id", testCfg.taskID,
		"upload_url", uploadResp.UploadUrl,
		"source_bucket", uploadResp.SourceBucket,
		"source_key", uploadResp.SourceKey,
		"expiry_seconds", uploadResp.ExpirySeconds)

	// 4. 使用预签名 URL 上传视频
	log.Info("Step 4: Uploading video via presigned URL")
	if err := uploadViaPresignedURL(ctx, log, testCfg.videoPath, uploadResp.UploadUrl); err != nil {
		result.Error = fmt.Sprintf("failed to upload video: %v", err)
		return result, fmt.Errorf("failed to upload video: %w", err)
	}
	log.Info("Video uploaded successfully via presigned URL")

	// 5. 提交转码任务
	log.Info("Step 5: Submitting transcoding task")
	if err := submitTask(ctx, log, testCfg, uploadResp.SourceBucket, uploadResp.SourceKey); err != nil {
		result.Error = fmt.Sprintf("failed to submit task: %v", err)
		return result, fmt.Errorf("failed to submit task: %w", err)
	}
	log.Info("Task submitted successfully")

	// 6. 轮询任务状态
	log.Info("Step 6: Polling task status")
	finalStatus, playlistURL, thumbnailURL, err := pollTaskStatus(ctx, log, testCfg, result)
	if err != nil {
		result.Error = fmt.Sprintf("task failed: %v", err)
		return result, fmt.Errorf("task failed: %w", err)
	}

	result.Status = finalStatus
	result.CompletedAt = time.Now()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)

	// 7. 使用从 API 返回的播放 URL
	if finalStatus == "success" {
		log.Info("Step 7: Using playback URLs from API")
		result.PlaylistURL = playlistURL
		result.ThumbnailURL = thumbnailURL
		log.Info("Playback URLs obtained from API",
			"playlist", result.PlaylistURL,
			"thumbnail", result.ThumbnailURL)
	}

	log.Info("E2E test completed successfully",
		"duration", result.Duration,
		"status", finalStatus)

	return result, nil
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

// getUploadURL 从 API 获取上传预签名 URL
func getUploadURL(ctx context.Context, log *slog.Logger, testCfg *TestConfig, fileName string, fileSize int64) (*pb.GetUploadURLResponse, error) {
	req := connect.NewRequest(&pb.GetUploadURLRequest{
		TaskId:   testCfg.taskID,
		FileName: fileName,
		FileSize: fileSize,
	})

	resp, err := testCfg.videoClient.GetUploadURL(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetUploadURL failed: %w", err)
	}

	log.Info("GetUploadURL response",
		"task_id", resp.Msg.TaskId,
		"upload_url", resp.Msg.UploadUrl,
		"source_bucket", resp.Msg.SourceBucket,
		"source_key", resp.Msg.SourceKey,
		"expiry_seconds", resp.Msg.ExpirySeconds)

	return resp.Msg, nil
}

// uploadViaPresignedURL 使用预签名 URL 上传文件
func uploadViaPresignedURL(ctx context.Context, log *slog.Logger, filePath, uploadURL string) error {
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

	// 创建 HTTP PUT 请求
	httpReq, err := http.NewRequestWithContext(ctx, "PUT", uploadURL, file)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.ContentLength = fileInfo.Size()

	// 检测 Content-Type
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	httpReq.Header.Set("Content-Type", contentType)

	log.Info("Uploading file via presigned URL",
		"url", uploadURL,
		"size", fileInfo.Size(),
		"content_type", contentType)

	// 执行请求
	client := &http.Client{
		Timeout: 30 * time.Minute, // 大文件上传需要较长时间
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upload failed with status code: %d", resp.StatusCode)
	}

	log.Info("File uploaded successfully via presigned URL", "status_code", resp.StatusCode)
	return nil
}

// pollTaskStatus 轮询任务状态
func pollTaskStatus(ctx context.Context, log *slog.Logger, testCfg *TestConfig, result *TestResult) (string, string, string, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", "", "", ctx.Err()
		case <-ticker.C:
			status, progress, errMsg, playlistURL, thumbnailURL, err := getTaskStatus(ctx, testCfg)
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
				"error", errMsg,
				"playlist_url", playlistURL,
				"thumbnail_url", thumbnailURL)

			// 检查终端状态
			switch status {
			case "success":
				return status, playlistURL, thumbnailURL, nil
			case "failed":
				return status, "", "", fmt.Errorf("task failed: %s", errMsg)
			case "cancelled":
				return status, "", "", fmt.Errorf("task was cancelled")
			}
		}
	}
}

// getTaskStatus 获取任务状态
func getTaskStatus(ctx context.Context, testCfg *TestConfig) (string, int, string, string, string, error) {
	req := connect.NewRequest(&pb.GetTaskStatusRequest{
		TaskId: testCfg.taskID,
	})

	resp, err := testCfg.videoClient.GetTaskStatus(ctx, req)
	if err != nil {
		return "", 0, "", "", "", err
	}

	return resp.Msg.Status, int(resp.Msg.Progress), resp.Msg.ErrorMessage, resp.Msg.PlaylistUrl, resp.Msg.ThumbnailUrl, nil
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
