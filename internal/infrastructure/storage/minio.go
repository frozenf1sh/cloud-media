package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/google/wire"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ProviderSet 是 Wire 的提供者集合
var ProviderSet = wire.NewSet(
	NewMinIOStorage,
	wire.Bind(new(domain.ObjectStorage), new(*MinIOStorage)),
)

const defaultRegion = "us-east-1"

// Config MinIO 配置（双 Endpoint 模式）
type Config struct {
	InternalEndpoint string // 内网地址
	InternalUseSSL   bool   // 内网是否使用 HTTPS
	ExternalEndpoint string // 外网地址
	ExternalUseSSL   bool   // 外网是否使用 HTTPS
	AccessKeyID      string // 访问密钥
	SecretAccessKey  string // 秘密密钥
}

// MinIOStorage MinIO 对象存储实现（双客户端模式）
type MinIOStorage struct {
	coreClient   *minio.Client // 内网客户端：用于上传、下载、管理
	signerClient *minio.Client // 外网客户端：仅用于生成预签名 URL
}

// NewMinIOStorage 创建 MinIOStorage 实例（双客户端模式）
func NewMinIOStorage(config *Config) (*MinIOStorage, error) {
	// 1. 初始化内网核心客户端（用于实际操作）
	coreOptions := &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.InternalUseSSL,
		Region: defaultRegion,
	}
	coreClient, err := minio.New(config.InternalEndpoint, coreOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create internal minio client: %w", err)
	}

	// 2. 初始化外网签名客户端（仅用于生成预签名 URL）
	signerOptions := &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.ExternalUseSSL,
		Region: defaultRegion,
	}
	signerClient, err := minio.New(config.ExternalEndpoint, signerOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer minio client: %w", err)
	}

	return &MinIOStorage{
		coreClient:   coreClient,
		signerClient: signerClient,
	}, nil
}

// UploadFile 上传文件到指定存储桶（使用内网客户端）
func (m *MinIOStorage) UploadFile(ctx context.Context, bucket, key string, data []byte) error {
	// 确保存储桶存在
	exists, err := m.coreClient.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		if err := m.coreClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	_, err = m.coreClient.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)),
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
func (m *MinIOStorage) DownloadFile(ctx context.Context, bucket, key string) ([]byte, error) {
	var buf bytes.Buffer
	obj, err := m.coreClient.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	defer obj.Close()
	if _, err = io.Copy(&buf, obj); err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}

	return buf.Bytes(), nil
}

// GetPresignedURL 获取预签名 URL（使用外网客户端，供外部用户访问）
func (m *MinIOStorage) GetPresignedURL(ctx context.Context, bucket, key string, method string, expiry int64) (string, error) {
	var u *url.URL
	var err error

	switch method {
	case "GET":
		u, err = m.signerClient.PresignedGetObject(ctx, bucket, key, time.Duration(expiry)*time.Second, url.Values{})
	case "PUT":
		u, err = m.signerClient.PresignedPutObject(ctx, bucket, key, time.Duration(expiry)*time.Second)
	default:
		err = fmt.Errorf("unsupported HTTP method: %s", method)
	}

	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return u.String(), nil
}

// ListObjects 列出存储桶中的对象（使用内网客户端）
func (m *MinIOStorage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	var objects []string

	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}

	for object := range m.coreClient.ListObjects(ctx, bucket, opts) {
		if object.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", object.Err)
		}
		objects = append(objects, object.Key)
	}

	return objects, nil
}

// DeleteObject 删除对象（使用内网客户端）
func (m *MinIOStorage) DeleteObject(ctx context.Context, bucket, key string) error {
	err := m.coreClient.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return nil
}

// UploadFromReader 从 io.Reader 上传文件（使用内网客户端）
func (m *MinIOStorage) UploadFromReader(ctx context.Context, bucket, key string, reader io.Reader, size int64) error {
	// 确保存储桶存在
	exists, err := m.coreClient.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		if err := m.coreClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	_, err = m.coreClient.PutObject(ctx, bucket, key, reader, size,
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
func (m *MinIOStorage) DownloadToWriter(ctx context.Context, bucket, key string, writer io.Writer) error {
	obj, err := m.coreClient.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to get object: %w", err)
	}
	defer obj.Close()

	if _, err = io.Copy(writer, obj); err != nil {
		return fmt.Errorf("failed to copy object to writer: %w", err)
	}

	return nil
}
