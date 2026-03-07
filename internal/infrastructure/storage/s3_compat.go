package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/google/wire"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ProviderSet 是 Wire 的提供者集合
var ProviderSet = wire.NewSet(
	NewS3CompatStorage,
	wire.Bind(new(domain.ObjectStorage), new(*S3CompatStorage)),
)

// S3CompatStorage S3 兼容对象存储实现（支持 MinIO、AWS S3、Cloudflare R2）
type S3CompatStorage struct {
	coreClient   *minio.Client // 内网客户端：用于上传、下载、管理
	signerClient *minio.Client // 外网客户端：仅用于生成预签名 URL
	cdnConfig    config.CDNConfig
}

// NewS3CompatStorage 创建 S3 兼容存储实例
func NewS3CompatStorage(cfg *config.ObjectStorageConfig) (*S3CompatStorage, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	logger.Debug("Initializing S3 compatible storage",
		logger.String("type", string(cfg.Type)),
		logger.String("internal_endpoint", cfg.InternalEndpoint),
		logger.String("internal_use_ssl", fmt.Sprintf("%t", cfg.InternalUseSSL)),
		logger.String("external_endpoint", cfg.ExternalEndpoint),
		logger.String("external_use_ssl", fmt.Sprintf("%t", cfg.ExternalUseSSL)),
		logger.String("region", region),
		logger.String("cdn_enabled", fmt.Sprintf("%t", cfg.CDN.Enabled)),
		logger.String("cdn_base_url", cfg.CDN.BaseURL),
	)

	// 1. 初始化内网核心客户端（用于实际操作）
	coreOptions := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.InternalUseSSL,
		Region: region,
	}
	coreClient, err := minio.New(cfg.InternalEndpoint, coreOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create internal storage client: %w", err)
	}
	logger.Debug("Internal storage client created", logger.String("endpoint", cfg.InternalEndpoint))

	// 2. 初始化外网签名客户端（仅用于生成预签名 URL）
	signerOptions := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.ExternalUseSSL,
		Region: region,
	}
	signerClient, err := minio.New(cfg.ExternalEndpoint, signerOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer storage client: %w", err)
	}
	logger.Debug("Signer storage client created", logger.String("endpoint", cfg.ExternalEndpoint))

	return &S3CompatStorage{
		coreClient:   coreClient,
		signerClient: signerClient,
		cdnConfig:    cfg.CDN,
	}, nil
}

// UploadFile 上传文件到指定存储桶（使用内网客户端）
func (s *S3CompatStorage) UploadFile(ctx context.Context, bucket, key string, data []byte) error {
	if err := s.ensureBucketExists(ctx, bucket); err != nil {
		return err
	}

	_, err := s.coreClient.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{
			ContentType: "application/octet-stream",
		},
	)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	return nil
}

// DownloadFile 下载文件（使用内网客户端）
func (s *S3CompatStorage) DownloadFile(ctx context.Context, bucket, key string) ([]byte, error) {
	var buf bytes.Buffer
	obj, err := s.coreClient.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	defer obj.Close()
	if _, err = io.Copy(&buf, obj); err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}

	return buf.Bytes(), nil
}

// GetPresignedURL 获取访问 URL（优先使用 CDN，回退到预签名 URL）
func (s *S3CompatStorage) GetPresignedURL(ctx context.Context, bucket, key string, method string, expiry int64) (string, error) {
	logger.DebugContext(ctx, "Generating presigned URL",
		logger.String("bucket", bucket),
		logger.String("key", key),
		logger.String("method", method),
		logger.Int64("expiry_seconds", expiry),
		logger.String("cdn_enabled", fmt.Sprintf("%t", s.cdnConfig.Enabled)),
	)

	// 如果启用了 CDN 且是 GET 请求，直接返回 CDN URL
	if s.cdnConfig.Enabled && method == "GET" {
		cdnURL := s.buildCDNURL(bucket, key)
		logger.DebugContext(ctx, "Using CDN URL", logger.String("url", cdnURL))
		return cdnURL, nil
	}

	// 否则使用预签名 URL
	var u *url.URL
	var err error

	switch method {
	case "GET":
		logger.DebugContext(ctx, "Generating presigned GET URL")
		u, err = s.signerClient.PresignedGetObject(ctx, bucket, key, time.Duration(expiry)*time.Second, url.Values{})
	case "PUT":
		logger.DebugContext(ctx, "Generating presigned PUT URL")
		u, err = s.signerClient.PresignedPutObject(ctx, bucket, key, time.Duration(expiry)*time.Second)
	default:
		err = fmt.Errorf("unsupported HTTP method: %s", method)
	}

	if err != nil {
		logger.ErrorContext(ctx, "Failed to generate presigned URL", logger.Err(err))
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	logger.DebugContext(ctx, "Raw presigned URL generated",
		logger.String("raw_url", u.String()),
		logger.String("scheme", u.Scheme),
		logger.String("host", u.Host),
		logger.String("path", u.Path),
	)

	// 去掉标准端口（http:80, https:443）
	originalHost := u.Host
	if (u.Scheme == "http" && u.Host == strings.TrimSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && u.Host == strings.TrimSuffix(u.Host, ":443")) {
		// 已经没有端口，无需处理
		logger.DebugContext(ctx, "No standard port to remove")
	} else if u.Scheme == "http" && strings.HasSuffix(u.Host, ":80") {
		u.Host = strings.TrimSuffix(u.Host, ":80")
		logger.DebugContext(ctx, "Removed port :80 from URL",
			logger.String("original_host", originalHost),
			logger.String("new_host", u.Host),
		)
	} else if u.Scheme == "https" && strings.HasSuffix(u.Host, ":443") {
		u.Host = strings.TrimSuffix(u.Host, ":443")
		logger.DebugContext(ctx, "Removed port :443 from URL",
			logger.String("original_host", originalHost),
			logger.String("new_host", u.Host),
		)
	}

	finalURL := u.String()
	logger.DebugContext(ctx, "Final presigned URL", logger.String("url", finalURL))
	return finalURL, nil
}

// ListObjects 列出存储桶中的对象（使用内网客户端）
func (s *S3CompatStorage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	var objects []string

	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}

	for object := range s.coreClient.ListObjects(ctx, bucket, opts) {
		if object.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", object.Err)
		}
		objects = append(objects, object.Key)
	}

	return objects, nil
}

// DeleteObject 删除对象（使用内网客户端）
func (s *S3CompatStorage) DeleteObject(ctx context.Context, bucket, key string) error {
	err := s.coreClient.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return nil
}

// UploadFromReader 从 io.Reader 上传文件（使用内网客户端）
func (s *S3CompatStorage) UploadFromReader(ctx context.Context, bucket, key string, reader io.Reader, size int64) error {
	if err := s.ensureBucketExists(ctx, bucket); err != nil {
		return err
	}

	_, err := s.coreClient.PutObject(ctx, bucket, key, reader, size,
		minio.PutObjectOptions{
			ContentType: "application/octet-stream",
		},
	)
	if err != nil {
		return fmt.Errorf("failed to upload from reader: %w", err)
	}

	return nil
}

// DownloadToWriter 下载文件到 io.Writer（使用内网客户端）
func (s *S3CompatStorage) DownloadToWriter(ctx context.Context, bucket, key string, writer io.Writer) error {
	obj, err := s.coreClient.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to get object: %w", err)
	}
	defer obj.Close()

	if _, err = io.Copy(writer, obj); err != nil {
		return fmt.Errorf("failed to copy object to writer: %w", err)
	}

	return nil
}

// EnsureBucketExists 确保存储桶存在，不存在则创建
func (s *S3CompatStorage) EnsureBucketExists(ctx context.Context, bucket string) error {
	return s.ensureBucketExists(ctx, bucket)
}

// ensureBucketExists 确保存储桶存在，不存在则创建（内部方法）
func (s *S3CompatStorage) ensureBucketExists(ctx context.Context, bucket string) error {
	logger.DebugContext(ctx, "Checking if bucket exists", logger.String("bucket", bucket))
	exists, err := s.coreClient.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		logger.InfoContext(ctx, "Bucket does not exist, creating it", logger.String("bucket", bucket))
		if err := s.coreClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		logger.InfoContext(ctx, "Bucket created successfully", logger.String("bucket", bucket))
	} else {
		logger.DebugContext(ctx, "Bucket already exists", logger.String("bucket", bucket))
	}
	return nil
}

// buildCDNURL 构建 CDN URL
func (s *S3CompatStorage) buildCDNURL(bucket, key string) string {
	// CDN URL 格式: {cdn_base_url}/{bucket}/{key}
	// 注意：实际生产中可能需要根据 CDN 提供商调整路径格式
	return fmt.Sprintf("%s/%s", s.cdnConfig.BaseURL, path.Join(bucket, key))
}
