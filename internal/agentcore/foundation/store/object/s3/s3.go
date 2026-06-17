package s3

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
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
	// PayloadSigningEnabled 是否签名 payload，默认 false
	// 对应 Python payload_signing_enabled=False，当为 false 时不签名请求体
	PayloadSigningEnabled bool
}

// S3Client 基于 aws-sdk-go-v2 的 S3 兼容对象存储客户端
//
// 支持华为云 OBS 以及任何 S3 兼容的对象存储服务。
// 客户端为长生命周期，并发安全，底层连接池自动管理。
// 使用分段上传（Multipart Upload）支持大文件上传。
//
// 对应 Python: openjiuwen/core/foundation/store/object/aioboto_storage_client.py
type S3Client struct {
	// client S3 服务客户端
	client *s3.Client
	// uploader 分段上传管理器
	uploader *manager.Uploader
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 对象存储日志组件，agentcore 下的包应使用 ComponentAgentCore
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewS3Client 创建 S3 兼容对象存储客户端
//
// 初始化流程：
//  1. 环境变量兜底：配置字段为空时从 OBS_SERVER / OBS_ACCESS_KEY_ID / OBS_SECRET_ACCESS_KEY / OBS_REGION 读取
//  2. 设置 AWS 校验和环境变量（对齐 Python: AWS_REQUEST_CHECKSUM_CALCULATION / AWS_RESPONSE_CHECKSUM_VALIDATION）
//  3. 构建 AWS 静态凭证
//  4. 加载 AWS 配置并自定义 endpoint（指向 Server URL）
//  5. 创建 S3 客户端，设置签名版本 v4、PayloadSigningEnabled: false
//  6. 创建分段上传管理器
func NewS3Client(cfg S3ClientConfig) (*S3Client, error) {
	// 环境变量兜底
	cfg.ApplyEnvFallback()

	// 设置 AWS 校验和环境变量，对齐 Python: os.environ["AWS_REQUEST_CHECKSUM_CALCULATION"] = "WHEN_REQUIRED"
	// 兼容新版 SDK 校验和计算行为变更，避免与 OBS/新版 S3 服务交互时 checksum 校验失败
	if err := os.Setenv("AWS_REQUEST_CHECKSUM_CALCULATION", "WHEN_REQUIRED"); err != nil {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("设置环境变量 AWS_REQUEST_CHECKSUM_CALCULATION 失败: %v", err)))
	}
	if err := os.Setenv("AWS_RESPONSE_CHECKSUM_VALIDATION", "WHEN_REQUIRED"); err != nil {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("设置环境变量 AWS_RESPONSE_CHECKSUM_VALIDATION 失败: %v", err)))
	}

	// 校验必填配置
	if cfg.Server == "" {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", "服务端点不能为空"))
	}
	if cfg.AccessKeyID == "" {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", "access_key_id 不能为空"))
	}
	if cfg.SecretAccessKey == "" {
		return nil, exception.BuildError(exception.StatusStoreObjectConfigInvalid,
			exception.WithParam("error_msg", "secret_access_key 不能为空"))
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
			exception.WithParam("error_msg", fmt.Sprintf("加载 AWS 配置失败: %v", err)))
	}

	// 构建 S3 客户端选项
	var clientOpts []func(*s3.Options)
	clientOpts = append(clientOpts, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Server)
		o.UsePathStyle = false // 虚拟主机风格寻址，对应 Python addressing_style="virtual"
		o.EndpointOptions.DisableHTTPS = false
	})

	// G-29: 实现 payload_signing_enabled=False
	// 当 PayloadSigningEnabled 为 false 时，使用 UNSIGNED-PAYLOAD 签名
	// 对齐 Python: Config(s3={"payload_signing_enabled": False})
	if !cfg.PayloadSigningEnabled {
		clientOpts = append(clientOpts, s3.WithAPIOptions(
			v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware,
		))
	}

	// 创建 S3 客户端
	client := s3.NewFromConfig(awsCfg, clientOpts...)

	// 创建分段上传管理器，对应 Python upload_fileobj 自动分段上传
	uploader := manager.NewUploader(client)

	logger.Info(logComponent).
		Str("server", cfg.Server).
		Str("region", cfg.RegionName).
		Bool("payload_signing_enabled", cfg.PayloadSigningEnabled).
		Msg("S3 客户端初始化成功")

	return &S3Client{client: client, uploader: uploader}, nil
}

// CreateBucket 创建新的对象存储桶
//
// 对应 Python: AioBotoClient.create_bucket
func (c *S3Client) CreateBucket(ctx context.Context, bucketName string, location string) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}
	// AWS S3 在 us-east-1 或空位置时不应传 CreateBucketConfiguration，
	// 否则会报 IllegalLocationConstraintException，对齐 T-21 修复。
	if location != "" && location != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(location),
		}
	}
	_, err := c.client.CreateBucket(ctx, input)
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
// 使用分段上传（Multipart Upload），大文件自动分片，对应 Python upload_fileobj。
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
			exception.WithParam("error_msg", fmt.Sprintf("打开文件失败: %v", err)))
	}
	defer func() { _ = file.Close() }()

	// G-31: 检查 file.Stat() 错误，避免 fileInfo 为 nil 时 panic
	fileInfo, err := file.Stat()
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "OBJECT_STORE_ERROR").
			Str("method", "UploadFile").
			Str("object_name", objectName).
			Str("bucket_name", bucketName).
			Str("file_path", filePath).
			Msg("获取文件信息失败")
		return exception.BuildError(exception.StatusStoreObjectUploadFailed,
			exception.WithParam("object_name", objectName),
			exception.WithParam("bucket_name", bucketName),
			exception.WithParam("error_msg", fmt.Sprintf("获取文件信息失败: %v", err)))
	}

	// G-30: 使用分段上传替代 PutObject，大文件自动分片
	_, err = c.uploader.Upload(ctx, &s3.PutObjectInput{
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
		Int64("file_size", fileInfo.Size()).
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
			exception.WithParam("error_msg", fmt.Sprintf("创建文件失败: %v", err)))
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
			exception.WithParam("error_msg", fmt.Sprintf("写入文件失败: %v", err)))
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
		// 逐对象 Debug 日志，对齐 Python: logger.debug for each object
		logger.Debug(logComponent).
			Str("bucket_name", bucketName).
			Str("key", aws.ToString(obj.Key)).
			Int64("size", aws.ToInt64(obj.Size)).
			Msg("列出对象")
	}

	logger.Info(logComponent).
		Str("bucket_name", bucketName).
		Int("object_count", len(objects)).
		Msg("列出对象成功")
	return objects, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
