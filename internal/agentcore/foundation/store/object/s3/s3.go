package s3

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	objectpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/object"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// S3ClientConfig S3 客户端特定配置
//
// 在 ObjectStorageConfig 基础上增加 S3 SDK 特有的配置项。
// 对应 Python 端 boto3 Config(signature_version="s3v4", s3={"payload_signing_enabled": False})
type S3ClientConfig struct {
	// ObjectStorageConfig 基础对象存储配置
	objectpkg.ObjectStorageConfig
	// SignatureVersion 签名版本，默认 "v4"
	SignatureVersion string
	// PayloadSigningEnabled 是否签名 payload，默认 false
	PayloadSigningEnabled bool
}

// S3Client 基于 aws-sdk-go-v2 的 S3 兼容对象存储客户端
//
// 支持华为云 OBS 以及任何 S3 兼容的对象存储服务。
// 客户端为长生命周期，并发安全，底层连接池自动管理。
//
// 对应 Python: openjiuwen/core/foundation/store/object/aioboto_storage_client.py
type S3Client struct {
	// client S3 服务客户端
	client *s3.Client
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 对象存储日志组件
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewS3Client 创建 S3 兼容对象存储客户端
//
// 初始化流程：
//  1. 环境变量兜底：配置字段为空时从 OBS_SERVER / OBS_ACCESS_KEY_ID / OBS_SECRET_ACCESS_KEY / OBS_REGION 读取
//  2. 构建 AWS 静态凭证
//  3. 加载 AWS 配置并自定义 endpoint（指向 Server URL）
//  4. 创建 S3 客户端，设置签名版本 v4、PayloadSigningEnabled: false
func NewS3Client(cfg S3ClientConfig) (*S3Client, error) {
	// 环境变量兜底
	cfg.ObjectStorageConfig.ApplyEnvFallback()

	// 校验必填配置
	if cfg.Server == "" {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", "server endpoint is required"))
	}
	if cfg.AccessKeyID == "" {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", "access_key_id is required"))
	}
	if cfg.SecretAccessKey == "" {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", "secret_access_key is required"))
	}

	// 构建 AWS 静态凭证
	credProvider := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")

	// 加载 AWS 配置
	awsCfg, err := awsConfig.LoadDefaultConfig(context.Background(),
		awsConfig.WithCredentialsProvider(credProvider),
		awsConfig.WithRegion(cfg.RegionName),
	)
	if err != nil {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("load aws config failed: %v", err)))
	}

	// 创建 S3 客户端，自定义 endpoint
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Server)
		o.UsePathStyle = false // 虚拟主机风格寻址，对应 Python addressing_style="virtual"
		o.EndpointOptions.DisableHTTPS = false
	})

	logger.Info(logComponent).
		Str("server", cfg.Server).
		Str("region", cfg.RegionName).
		Msg("S3 客户端初始化成功")

	return &S3Client{client: client}, nil
}

// CreateBucket 创建新的对象存储桶
//
// 对应 Python: AioBotoClient.create_bucket
func (c *S3Client) CreateBucket(ctx context.Context, bucketName string, location string) error {
	_, err := c.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
		CreateBucketConfiguration: &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(location),
		},
	})
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "CreateBucket").
			Str("bucket_name", bucketName).
			Str("location", location).
			Msg("桶创建失败")
		return exception.BuildError(exception.StatusStoreObjectBucketCreateFailed,
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", err.Error()))
	}

	logger.Info(logComponent).
		Str("bucket_name", bucketName).
		Str("location", location).
		Msg("桶创建成功")
	return nil
}

// DeleteBucket 删除已有的对象存储桶
//
// 对应 Python: AioBotoClient.delete_bucket
func (c *S3Client) DeleteBucket(ctx context.Context, bucketName string) error {
	_, err := c.client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "DeleteBucket").
			Str("bucket_name", bucketName).
			Msg("桶删除失败")
		return exception.BuildError(exception.StatusStoreObjectBucketDeleteFailed,
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", err.Error()))
	}

	logger.Info(logComponent).
		Str("bucket_name", bucketName).
		Msg("桶删除成功")
	return nil
}

// UploadFile 上传本地文件到对象存储桶
//
// 对应 Python: AioBotoClient.upload_file
func (c *S3Client) UploadFile(ctx context.Context, bucketName string, objectName string, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "UploadFile").
			Str("object_name", objectName).
			Str("bucket_name", bucketName).
			Str("file_path", filePath).
			Msg("上传文件打开失败")
		return exception.BuildError(exception.StatusStoreObjectUploadFailed,
			exception.WithParam("object_name", objectName),
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", fmt.Sprintf("open file failed: %v", err)))
	}
	defer func() { _ = file.Close() }()

	fileInfo, _ := file.Stat()
	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucketName),
		Key:           aws.String(objectName),
		Body:          file,
		ContentLength: aws.Int64(fileInfo.Size()),
	})
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "UploadFile").
			Str("object_name", objectName).
			Str("bucket_name", bucketName).
			Str("file_path", filePath).
			Msg("文件上传失败")
		return exception.BuildError(exception.StatusStoreObjectUploadFailed,
			exception.WithParam("object_name", objectName),
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", err.Error()))
	}

	logger.Info(logComponent).
		Str("object_name", objectName).
		Str("file_path", filePath).
		Str("bucket_name", bucketName).
		Msg("文件上传成功")
	return nil
}

// DownloadFile 从对象存储下载文件到本地
//
// 对应 Python: AioBotoClient.download_file
func (c *S3Client) DownloadFile(ctx context.Context, bucketName string, objectName string, filePath string) error {
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectName),
	})
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "DownloadFile").
			Str("object_name", objectName).
			Str("bucket_name", bucketName).
			Msg("文件下载失败")
		return exception.BuildError(exception.StatusStoreObjectDownloadFailed,
			exception.WithParam("object_name", objectName),
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", err.Error()))
	}
	defer func() { _ = result.Body.Close() }()

	file, err := os.Create(filePath)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "DownloadFile").
			Str("object_name", objectName).
			Str("bucket_name", bucketName).
			Str("file_path", filePath).
			Msg("下载文件创建失败")
		return exception.BuildError(exception.StatusStoreObjectDownloadFailed,
			exception.WithParam("object_name", objectName),
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", fmt.Sprintf("create file failed: %v", err)))
	}
	defer func() { _ = file.Close() }()

	// 将响应体写入本地文件
	_, err = file.ReadFrom(result.Body)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "DownloadFile").
			Str("object_name", objectName).
			Str("bucket_name", bucketName).
			Str("file_path", filePath).
			Msg("下载文件写入失败")
		return exception.BuildError(exception.StatusStoreObjectDownloadFailed,
			exception.WithParam("object_name", objectName),
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", fmt.Sprintf("write file failed: %v", err)))
	}

	logger.Info(logComponent).
		Str("object_name", objectName).
		Str("bucket_name", bucketName).
		Str("file_path", filePath).
		Msg("文件下载成功")
	return nil
}

// DeleteObject 删除对象存储中的对象
//
// 对应 Python: AioBotoClient.delete_object
func (c *S3Client) DeleteObject(ctx context.Context, bucketName string, objectName string) error {
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectName),
	})
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "DeleteObject").
			Str("object_name", objectName).
			Str("bucket_name", bucketName).
			Msg("对象删除失败")
		return exception.BuildError(exception.StatusStoreObjectDeleteFailed,
			exception.WithParam("object_name", objectName),
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", err.Error()))
	}

	logger.Info(logComponent).
		Str("object_name", objectName).
		Str("bucket_name", bucketName).
		Msg("对象删除成功")
	return nil
}

// ListObjects 列出指定前缀的对象
//
// 不传 WithMaxObjects 时默认返回最多 100 个对象。
// 对应 Python: AioBotoClient.list_objects
func (c *S3Client) ListObjects(ctx context.Context, bucketName string, objectPrefix string, opts ...objectpkg.ListOption) ([]map[string]any, error) {
	listOpts := objectpkg.NewListOptions(opts...)

	result, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucketName),
		Prefix:  aws.String(objectPrefix),
		MaxKeys: aws.Int32(int32(listOpts.MaxObjects)),
	})
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "ListObjects").
			Str("bucket_name", bucketName).
			Msg("列出对象失败")
		return nil, exception.BuildError(exception.StatusStoreObjectListFailed,
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", err.Error()))
	}

	objects := make([]map[string]any, 0, len(result.Contents))
	for _, obj := range result.Contents {
		m := map[string]any{
			"Key":          aws.ToString(obj.Key),
			"LastModified": obj.LastModified,
			"Size":         aws.ToInt64(obj.Size),
			"ETag":         aws.ToString(obj.ETag),
			"StorageClass": string(obj.StorageClass),
		}
		objects = append(objects, m)
	}

	logger.Info(logComponent).
		Str("bucket_name", bucketName).
		Int("object_count", len(objects)).
		Msg("列出对象成功")
	return objects, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
