package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/config"
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
	// 如果启用了 CDN 且是 GET 请求，直接返回 CDN URL
	if s.cdnConfig.Enabled && method == "GET" {
		return s.buildCDNURL(bucket, key), nil
	}

	// 否则使用预签名 URL
	var u *url.URL
	var err error

	switch method {
	case "GET":
		u, err = s.signerClient.PresignedGetObject(ctx, bucket, key, time.Duration(expiry)*time.Second, url.Values{})
	case "PUT":
		u, err = s.signerClient.PresignedPutObject(ctx, bucket, key, time.Duration(expiry)*time.Second)
	default:
		err = fmt.Errorf("unsupported HTTP method: %s", method)
	}

	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return u.String(), nil
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

// ensureBucketExists 确保存储桶存在，不存在则创建
func (s *S3CompatStorage) ensureBucketExists(ctx context.Context, bucket string) error {
	exists, err := s.coreClient.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		if err := s.coreClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}
	return nil
}

// buildCDNURL 构建 CDN URL
func (s *S3CompatStorage) buildCDNURL(bucket, key string) string {
	// CDN URL 格式: {cdn_base_url}/{bucket}/{key}
	// 注意：实际生产中可能需要根据 CDN 提供商调整路径格式
	return fmt.Sprintf("%s/%s", s.cdnConfig.BaseURL, path.Join(bucket, key))
}
