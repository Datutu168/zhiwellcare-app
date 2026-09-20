package objectstore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Store 是 S3 兼容实现（MinIO / 阿里云 OSS / 腾讯云 COS / AWS S3）。
type S3Store struct {
	client     *minio.Client
	bucket     string
	publicBase string
}

// NewS3 建立 S3 客户端并确认桶可访问（桶不存在时尝试创建）。
func NewS3(ctx context.Context, opts Options) (*S3Store, error) {
	client, err := minio.New(opts.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure: opts.UseSSL,
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("S3 客户端初始化失败: %w", err)
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exists, err := client.BucketExists(checkCtx, opts.Bucket)
	if err != nil {
		return nil, fmt.Errorf("S3 桶探测失败: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(checkCtx, opts.Bucket, minio.MakeBucketOptions{Region: opts.Region}); err != nil {
			return nil, fmt.Errorf("S3 桶不存在且创建失败: %w", err)
		}
	}
	return &S3Store{client: client, bucket: opts.Bucket, publicBase: opts.PublicBase}, nil
}

func (s *S3Store) Kind() string { return "s3" }

func (s *S3Store) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	info, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("S3 上传失败: %w", err)
	}
	return ObjectInfo{
		Key:         info.Key,
		Size:        info.Size,
		ContentType: contentType,
		ETag:        info.ETag,
		ModifiedAt:  time.Now(),
	}, nil
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, fmt.Errorf("S3 下载失败: %w", err)
	}
	stat, err := object.Stat()
	if err != nil {
		_ = object.Close()
		// 对象不存在等错误统一映射为未找到，便于上层返回 404。
		return nil, ObjectInfo{}, fmt.Errorf("%w: %v", ErrObjectNotFound, err)
	}
	return object, ObjectInfo{
		Key:         stat.Key,
		Size:        stat.Size,
		ContentType: stat.ContentType,
		ETag:        stat.ETag,
		ModifiedAt:  stat.LastModified,
	}, nil
}

func (s *S3Store) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("%w: %v", ErrObjectNotFound, err)
	}
	return ObjectInfo{
		Key:         stat.Key,
		Size:        stat.Size,
		ContentType: stat.ContentType,
		ETag:        stat.ETag,
		ModifiedAt:  stat.LastModified,
	}, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *S3Store) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	presigned, err := s.client.PresignedPutObject(ctx, s.bucket, key, ttl)
	if err != nil {
		return "", fmt.Errorf("生成上传地址失败: %w", err)
	}
	return presigned.String(), nil
}

func (s *S3Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	presigned, err := s.client.PresignedGetObject(ctx, s.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("生成下载地址失败: %w", err)
	}
	return presigned.String(), nil
}

// PublicURL 配置了 CDN 前缀时优先返回 CDN 地址（生产直连 CDN，不消耗预签名）。
func (s *S3Store) PublicURL(key string) string { return JoinPublic(s.publicBase, key) }

func (s *S3Store) HealthCheck(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.bucket)
	return err
}

func (s *S3Store) Close() error { return nil }
