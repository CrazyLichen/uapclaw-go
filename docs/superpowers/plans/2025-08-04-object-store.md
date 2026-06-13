# Object Store 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现对象存储（Object Store）模块，基于 aws-sdk-go-v2 提供 S3 兼容的对象存储客户端，对齐 Python 端 aioboto_storage_client.py 的 6 个接口方法。

**Architecture:** 分层子包架构——`object/base.go` 定义 `BaseObjectStorage` 接口 + `ObjectStorageConfig` 配置 + `ListOption` 选项，`object/s3/` 子包提供 `S3Client` 基于 aws-sdk-go-v2 的 S3 兼容实现。客户端为长生命周期、并发安全，配置支持结构体字段优先、环境变量兜底。

**Tech Stack:** aws-sdk-go-v2 (service/s3, config, credentials), zerolog (日志), httptest (测试模拟)

**设计文档：** `docs/superpowers/specs/2025-08-04-object-store-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `go.mod` / `go.sum` | 添加 aws-sdk-go-v2 依赖 |
| 修改 | `internal/common/exception/codes_framework.go` | 添加对象存储错误码（186010–186019） |
| 创建 | `internal/agentcore/store/object/doc.go` | 包文档 |
| 创建 | `internal/agentcore/store/object/base.go` | BaseObjectStorage 接口 + ObjectStorageConfig + ListOption |
| 创建 | `internal/agentcore/store/object/base_test.go` | 接口与配置的单元测试 |
| 创建 | `internal/agentcore/store/object/s3/doc.go` | S3 子包文档 |
| 创建 | `internal/agentcore/store/object/s3/s3.go` | S3Client 实现 |
| 创建 | `internal/agentcore/store/object/s3/s3_test.go` | S3Client 单元测试（httptest 模拟） |

---

### Task 1: 添加 aws-sdk-go-v2 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 添加 aws-sdk-go-v2 依赖**

```bash
cd /home/opensource/uap-claw-go && go get github.com/aws/aws-sdk-go-v2 github.com/aws/aws-sdk-go-v2/config github.com/aws/aws-sdk-go-v2/credentials github.com/aws/aws-sdk-go-v2/service/s3 github.com/aws/aws-sdk-go-v2/feature/s3/manager
```

- [ ] **Step 2: 验证依赖添加成功**

```bash
cd /home/opensource/uap-claw-go && go mod tidy && grep "aws-sdk-go-v2" go.mod
```

Expected: 输出包含 `github.com/aws/aws-sdk-go-v2` 及其子模块

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum && git commit -m "chore: add aws-sdk-go-v2 dependencies for Object Store"
```

---

### Task 2: 添加对象存储错误码

**Files:**
- Modify: `internal/common/exception/codes_framework.go`

- [ ] **Step 1: 在 186 段（Store supporting）追加对象存储错误码**

在 `StatusStoreGraphCollectionNotSupported` 之后追加：

```go
	// StatusStoreObjectBucketCreateFailed 对象存储桶创建失败
	StatusStoreObjectBucketCreateFailed = NewStatusCode(
		"STORE_OBJECT_BUCKET_CREATE_FAILED", 186010,
		"store object bucket create failed, bucket={bucket_name}, reason: {error_msg}")
	// StatusStoreObjectBucketDeleteFailed 对象存储桶删除失败
	StatusStoreObjectBucketDeleteFailed = NewStatusCode(
		"STORE_OBJECT_BUCKET_DELETE_FAILED", 186011,
		"store object bucket delete failed, bucket={bucket_name}, reason: {error_msg}")
	// StatusStoreObjectUploadFailed 对象上传失败
	StatusStoreObjectUploadFailed = NewStatusCode(
		"STORE_OBJECT_UPLOAD_FAILED", 186012,
		"store object upload failed, object={object_name}, bucket={bucket_name}, reason: {error_msg}")
	// StatusStoreObjectDownloadFailed 对象下载失败
	StatusStoreObjectDownloadFailed = NewStatusCode(
		"STORE_OBJECT_DOWNLOAD_FAILED", 186013,
		"store object download failed, object={object_name}, bucket={bucket_name}, reason: {error_msg}")
	// StatusStoreObjectDeleteFailed 对象删除失败
	StatusStoreObjectDeleteFailed = NewStatusCode(
		"STORE_OBJECT_DELETE_FAILED", 186014,
		"store object delete failed, object={object_name}, bucket={bucket_name}, reason: {error_msg}")
	// StatusStoreObjectListFailed 对象列表查询失败
	StatusStoreObjectListFailed = NewStatusCode(
		"STORE_OBJECT_LIST_FAILED", 186015,
		"store object list failed, bucket={bucket_name}, reason: {error_msg}")
	// StatusStoreObjectConfigInvalid 对象存储配置无效
	StatusStoreObjectConfigInvalid = NewStatusCode(
		"STORE_OBJECT_CONFIG_INVALID", 186016,
		"store object config is invalid, reason: {error_msg}")
```

- [ ] **Step 2: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/common/exception/...
```

Expected: 无错误

- [ ] **Step 3: 提交**

```bash
git add internal/common/exception/codes_framework.go && git commit -m "feat(exception): 添加对象存储错误码 186010–186016"
```

---

### Task 3: 实现 object 包接口与配置（base.go + doc.go）

**Files:**
- Create: `internal/agentcore/store/object/doc.go`
- Create: `internal/agentcore/store/object/base.go`

- [ ] **Step 1: 创建 `internal/agentcore/store/object/` 目录**

```bash
mkdir -p /home/opensource/uap-claw-go/internal/agentcore/store/object
```

- [ ] **Step 2: 创建 doc.go**

```go
// Package object 提供对象存储的抽象接口定义和配置。
//
// 本包定义了所有对象存储后端必须满足的 BaseObjectStorage 接口，
// 以及通用的 ObjectStorageConfig 配置和 ListOption 列表查询选项。
// 具体后端实现由 s3 子包提供。
//
// 文件目录：
//
//	object/
//	├── doc.go     # 包文档
//	└── base.go    # BaseObjectStorage 接口 + ObjectStorageConfig + ListOption
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/object/
//
// 核心类型/接口索引：
//
//	BaseObjectStorage  — 对象存储客户端接口，定义上传/下载/删除/桶操作/列表查询
//	ObjectStorageConfig — 对象存储配置，支持结构体字段优先 + 环境变量兜底
//	ListOption         — 列表查询选项，WithMaxObjects 设置最大返回数（默认 100）
package object
```

- [ ] **Step 3: 创建 base.go**

```go
package object

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// BaseObjectStorage 对象存储客户端接口
//
// 定义对象存储的核心操作：文件上传/下载、对象删除、桶的创建/删除、对象列表查询。
// 所有方法均接收 context.Context 以支持超时和取消。
//
// 对应 Python: openjiuwen/core/foundation/store/object/base_storage_client.py
type BaseObjectStorage interface {
	// UploadFile 上传本地文件到对象存储桶
	UploadFile(ctx context.Context, bucketName string, objectName string, filePath string) error
	// DownloadFile 从对象存储下载文件到本地
	DownloadFile(ctx context.Context, bucketName string, objectName string, filePath string) error
	// DeleteObject 删除对象存储中的对象
	DeleteObject(ctx context.Context, bucketName string, objectName string) error
	// CreateBucket 创建新的对象存储桶
	CreateBucket(ctx context.Context, bucketName string, location string) error
	// DeleteBucket 删除已有的对象存储桶
	DeleteBucket(ctx context.Context, bucketName string) error
	// ListObjects 列出指定前缀的对象
	// 不传 WithMaxObjects 时默认返回最多 100 个对象
	ListObjects(ctx context.Context, bucketName string, objectPrefix string, opts ...ListOption) ([]map[string]any, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// ObjectStorageConfig 对象存储配置
//
// 字段为空时自动从环境变量读取：
//   - Server → OBS_SERVER
//   - AccessKeyID → OBS_ACCESS_KEY_ID
//   - SecretAccessKey → OBS_SECRET_ACCESS_KEY
//   - RegionName → OBS_REGION
//
// 优先级：结构体字段 > 环境变量
type ObjectStorageConfig struct {
	// Server S3 兼容服务端点 URL（如 https://obs.cn-north-4.myhuaweicloud.com）
	Server string
	// AccessKeyID 访问密钥 ID
	AccessKeyID string
	// SecretAccessKey 访问密钥
	SecretAccessKey string
	// RegionName 区域名称
	RegionName string
}

// listOptions 列表查询内部选项
type listOptions struct {
	maxObjects int
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxObjects ListObjects 默认最大返回对象数
	defaultMaxObjects = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 对象存储日志组件
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ListOption 列表查询选项
type ListOption func(*listOptions)

// WithMaxObjects 设置最大返回对象数，默认 100
func WithMaxObjects(n int) ListOption {
	return func(o *listOptions) {
		o.maxObjects = n
	}
}

// newListOptions 应用选项并返回默认值
func newListOptions(opts ...ListOption) listOptions {
	o := listOptions{
		maxObjects: defaultMaxObjects,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
```

- [ ] **Step 4: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/object/...
```

Expected: 无错误

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/object/ && git commit -m "feat(store/object): 添加 BaseObjectStorage 接口、ObjectStorageConfig 和 ListOption"
```

---

### Task 4: 编写 object/base_test.go 单元测试

**Files:**
- Create: `internal/agentcore/store/object/base_test.go`

- [ ] **Step 1: 编写 base_test.go**

```go
package object

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithMaxObjects(t *testing.T) {
	opts := newListOptions(WithMaxObjects(50))
	assert.Equal(t, 50, opts.maxObjects)
}

func TestListOptions_Default(t *testing.T) {
	opts := newListOptions()
	assert.Equal(t, 100, opts.maxObjects)
}

func TestListOptions_Multiple(t *testing.T) {
	opts := newListOptions(WithMaxObjects(200), WithMaxObjects(300))
	// 最后一个 WithMaxObjects 生效
	assert.Equal(t, 300, opts.maxObjects)
}

func TestObjectStorageConfig_EnvFallback(t *testing.T) {
	// 设置环境变量
	os.Setenv("OBS_SERVER", "https://obs.example.com")
	os.Setenv("OBS_ACCESS_KEY_ID", "test-ak")
	os.Setenv("OBS_SECRET_ACCESS_KEY", "test-sk")
	os.Setenv("OBS_REGION", "cn-north-4")
	defer func() {
		os.Unsetenv("OBS_SERVER")
		os.Unsetenv("OBS_ACCESS_KEY_ID")
		os.Unsetenv("OBS_SECRET_ACCESS_KEY")
		os.Unsetenv("OBS_REGION")
	}()

	cfg := ObjectStorageConfig{}
	cfg.ApplyEnvFallback()

	assert.Equal(t, "https://obs.example.com", cfg.Server)
	assert.Equal(t, "test-ak", cfg.AccessKeyID)
	assert.Equal(t, "test-sk", cfg.SecretAccessKey)
	assert.Equal(t, "cn-north-4", cfg.RegionName)
}

func TestObjectStorageConfig_StructOverEnv(t *testing.T) {
	os.Setenv("OBS_SERVER", "https://env.example.com")
	defer os.Unsetenv("OBS_SERVER")

	cfg := ObjectStorageConfig{
		Server: "https://struct.example.com",
	}
	cfg.ApplyEnvFallback()

	// 结构体字段优先，不从环境变量读取
	assert.Equal(t, "https://struct.example.com", cfg.Server)
}

func TestObjectStorageConfig_PartialEnvFallback(t *testing.T) {
	os.Setenv("OBS_SERVER", "https://env.example.com")
	os.Setenv("OBS_REGION", "cn-south-1")
	defer func() {
		os.Unsetenv("OBS_SERVER")
		os.Unsetenv("OBS_REGION")
	}()

	cfg := ObjectStorageConfig{
		AccessKeyID: "struct-ak",
	}
	cfg.ApplyEnvFallback()

	// 非空字段保留结构体值，空字段从环境变量读取
	assert.Equal(t, "https://env.example.com", cfg.Server)
	assert.Equal(t, "struct-ak", cfg.AccessKeyID)
	assert.Equal(t, "cn-south-1", cfg.RegionName)
}
```

- [ ] **Step 2: 在 base.go 中添加 ApplyEnvFallback 方法（测试需要）**

在 `base.go` 的导出函数区块末尾追加：

```go
// ApplyEnvFallback 对配置中为空的字段从环境变量读取
func (c *ObjectStorageConfig) ApplyEnvFallback() {
	if c.Server == "" {
		c.Server = os.Getenv("OBS_SERVER")
	}
	if c.AccessKeyID == "" {
		c.AccessKeyID = os.Getenv("OBS_ACCESS_KEY_ID")
	}
	if c.SecretAccessKey == "" {
		c.SecretAccessKey = os.Getenv("OBS_SECRET_ACCESS_KEY")
	}
	if c.RegionName == "" {
		c.RegionName = os.Getenv("OBS_REGION")
	}
}
```

同时在 `base.go` 的 import 中添加 `"os"`。

- [ ] **Step 3: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/store/object/ -run "TestWithMaxObjects|TestListOptions|TestObjectStorageConfig"
```

Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/object/ && git commit -m "test(store/object): 添加 ListOption 和 ObjectStorageConfig 单元测试"
```

---

### Task 5: 实现 S3Client 核心结构体与桶操作

**Files:**
- Create: `internal/agentcore/store/object/s3/doc.go`
- Create: `internal/agentcore/store/object/s3/s3.go`

- [ ] **Step 1: 创建 `s3/` 子包目录**

```bash
mkdir -p /home/opensource/uap-claw-go/internal/agentcore/store/object/s3
```

- [ ] **Step 2: 创建 s3/doc.go**

```go
// Package s3 提供对象存储的 S3 兼容后端实现。
//
// 基于 aws-sdk-go-v2 实现 S3 兼容的对象存储客户端，
// 支持华为云 OBS 以及任何 S3 兼容的对象存储服务。
// 客户端为长生命周期，并发安全，底层连接池自动管理。
//
// 文件目录：
//
//	s3/
//	├── doc.go     # 包文档
//	└── s3.go      # S3Client 实现（NewS3Client + 6 个接口方法）
//
// 对应 Python 代码：openjiuwen/core/foundation/store/object/aioboto_storage_client.py
package s3
```

- [ ] **Step 3: 创建 s3/s3.go，包含 S3Client 结构体、构造函数和桶操作方法**

```go
package s3

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	objectpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/store/object"
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
	client *s3.Client
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 对象存储日志组件
	logComponent = logger.ComponentCommon
)

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
	defer file.Close()

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
	defer result.Body.Close()

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
	defer file.Close()

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
		Bucket:    aws.String(bucketName),
		Prefix:    aws.String(objectPrefix),
		MaxKeys:   aws.Int32(int32(listOpts.MaxObjects)),
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
```

- [ ] **Step 4: 在 base.go 中导出 NewListOptions 函数（s3 子包需要调用）**

将 `base.go` 中的 `newListOptions` 改为导出函数 `NewListOptions`：

```go
// NewListOptions 应用选项并返回默认值
func NewListOptions(opts ...ListOption) listOptions {
	o := listOptions{
		maxObjects: defaultMaxObjects,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
```

同时删除原来的 `newListOptions` 函数。更新 `base_test.go` 中对 `newListOptions` 的调用为 `NewListOptions`。

- [ ] **Step 5: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/object/...
```

Expected: 无错误

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/object/ && git commit -m "feat(store/object/s3): 实现 S3Client 核心（构造函数 + 6 个接口方法）"
```

---

### Task 6: 编写 S3Client 单元测试（httptest 模拟）

**Files:**
- Create: `internal/agentcore/store/object/s3/s3_test.go`

- [ ] **Step 1: 编写 s3_test.go**

```go
package s3

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	objectpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/store/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestS3Client 创建基于 httptest 的 S3Client 用于测试
func newTestS3Client(handler http.Handler) (*S3Client, *httptest.Server) {
	server := httptest.NewServer(handler)

	credProvider := credentials.NewStaticCredentialsProvider("test-ak", "test-sk", "")
	awsCfg, _ := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(credProvider),
		config.WithRegion("us-east-1"),
	)

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true // 测试时使用路径风格
	})

	return &S3Client{client: client}, server
}

// mockS3Handler 模拟 S3 API 的 HTTP 处理器
type mockS3Handler struct {
	buckets  map[string]bool
	objects  map[string]map[string][]byte // bucket -> key -> content
	failOp   string                       // 设置此字段使指定操作返回错误
}

func newMockS3Handler() *mockS3Handler {
	return &mockS3Handler{
		buckets: make(map[string]bool),
		objects: make(map[string]map[string][]byte),
	}
}

func (h *mockS3Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 如果设置了 failOp，对应操作返回 500 错误
	if h.failOp != "" {
		if strings.Contains(r.URL.Path, h.failOp) || r.Method == "PUT" && h.failOp == "upload" {
			writeS3Error(w, "InternalError", "mock error")
			return
		}
	}

	switch r.Method {
	case http.MethodPut:
		// 创建桶或上传对象
		bucket := strings.Split(strings.Trim(r.URL.Path, "/"), "/")[0]
		parts := strings.SplitN(strings.Trim(r.URL.Path, "/"), "/", 2)
		if len(parts) == 1 || parts[1] == "" {
			// 创建桶
			h.buckets[bucket] = true
			h.objects[bucket] = make(map[string][]byte)
			w.WriteHeader(http.StatusOK)
		} else {
			// 上传对象
			key := parts[1]
			body, _ := io.ReadAll(r.Body)
			if h.objects[bucket] == nil {
				h.objects[bucket] = make(map[string][]byte)
			}
			h.objects[bucket][key] = body
			w.WriteHeader(http.StatusOK)
		}

	case http.MethodGet:
		parts := strings.SplitN(strings.Trim(r.URL.Path, "/"), "/", 2)
		bucket := parts[0]
		if len(parts) == 1 || parts[1] == "" {
			// ListObjects
			prefix := r.URL.Query().Get("prefix")
			result := listBucketResult{
				IsTruncated: false,
			}
			for key, content := range h.objects[bucket] {
				if prefix == "" || strings.HasPrefix(key, prefix) {
					result.Contents = append(result.Contents, objectItem{
						Key:          key,
						LastModified: time.Now().Format(time.RFC3339),
						Size:         len(content),
						ETag:         fmt.Sprintf(`"%x"`, len(content)),
					})
				}
			}
			w.Header().Set("Content-Type", "application/xml")
			xml.NewEncoder(w).Encode(result)
		} else {
			// GetObject
			key := parts[1]
			content, ok := h.objects[bucket][key]
			if !ok {
				writeS3Error(w, "NoSuchKey", "object not found")
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.Write(content)
		}

	case http.MethodDelete:
		parts := strings.SplitN(strings.Trim(r.URL.Path, "/"), "/", 2)
		bucket := parts[0]
		if len(parts) == 1 || parts[1] == "" {
			// 删除桶
			delete(h.buckets, bucket)
			delete(h.objects, bucket)
			w.WriteHeader(http.StatusNoContent)
		} else {
			// 删除对象
			key := parts[1]
			delete(h.objects[bucket], key)
			w.WriteHeader(http.StatusNoContent)
		}

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// S3 XML 响应类型
type listBucketResult struct {
	XMLName     xml.Name     `xml:"ListBucketResult"`
	IsTruncated bool         `xml:"IsTruncated"`
	Contents    []objectItem `xml:"Contents"`
}

type objectItem struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	Size         int    `xml:"Size"`
	ETag         string `xml:"ETag"`
}

type errorResult struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
}

func writeS3Error(w http.ResponseWriter, code, message string) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Set("Content-Type", "application/xml")
	xml.NewEncoder(w).Encode(errorResult{Code: code, Message: message})
}

// ──────────────────────────── CreateBucket 测试 ────────────────────────────

func TestS3Client_CreateBucket(t *testing.T) {
	mock := newMockS3Handler()
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.CreateBucket(context.Background(), "test-bucket", "us-east-1")
	assert.NoError(t, err)
	assert.True(t, mock.buckets["test-bucket"])
}

func TestS3Client_CreateBucket_失败(t *testing.T) {
	mock := newMockS3Handler()
	mock.failOp = "create-bucket"
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.CreateBucket(context.Background(), "test-bucket", "us-east-1")
	assert.Error(t, err)
}

// ──────────────────────────── DeleteBucket 测试 ────────────────────────────

func TestS3Client_DeleteBucket(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.DeleteBucket(context.Background(), "test-bucket")
	assert.NoError(t, err)
	assert.False(t, mock.buckets["test-bucket"])
}

func TestS3Client_DeleteBucket_失败(t *testing.T) {
	mock := newMockS3Handler()
	mock.failOp = "delete-bucket"
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.DeleteBucket(context.Background(), "nonexistent-bucket")
	assert.Error(t, err)
}

// ──────────────────────────── UploadFile 测试 ────────────────────────────

func TestS3Client_UploadFile(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	// 创建临时测试文件
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("hello world"), 0644)
	require.NoError(t, err)

	err = client.UploadFile(context.Background(), "test-bucket", "test.txt", tmpFile)
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello world"), mock.objects["test-bucket"]["test.txt"])
}

func TestS3Client_UploadFile_文件不存在(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.UploadFile(context.Background(), "test-bucket", "test.txt", "/nonexistent/file.txt")
	assert.Error(t, err)
}

// ──────────────────────────── DownloadFile 测试 ────────────────────────────

func TestS3Client_DownloadFile(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = map[string][]byte{
		"test.txt": []byte("download content"),
	}
	client, server := newTestS3Client(mock)
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "downloaded.txt")

	err := client.DownloadFile(context.Background(), "test-bucket", "test.txt", tmpFile)
	assert.NoError(t, err)

	content, err := os.ReadFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "download content", string(content))
}

func TestS3Client_DownloadFile_文件不存在(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "downloaded.txt")

	err := client.DownloadFile(context.Background(), "test-bucket", "nonexistent.txt", tmpFile)
	assert.Error(t, err)
}

// ──────────────────────────── DeleteObject 测试 ────────────────────────────

func TestS3Client_DeleteObject(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = map[string][]byte{
		"test.txt": []byte("content"),
	}
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.DeleteObject(context.Background(), "test-bucket", "test.txt")
	assert.NoError(t, err)
	_, exists := mock.objects["test-bucket"]["test.txt"]
	assert.False(t, exists)
}

func TestS3Client_DeleteObject_失败(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	mock.failOp = "delete"
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.DeleteObject(context.Background(), "test-bucket", "test.txt")
	assert.Error(t, err)
}

// ──────────────────────────── ListObjects 测试 ────────────────────────────

func TestS3Client_ListObjects(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = map[string][]byte{
		"prefix/a.txt": []byte("a"),
		"prefix/b.txt": []byte("bb"),
		"other/c.txt":  []byte("ccc"),
	}
	client, server := newTestS3Client(mock)
	defer server.Close()

	objects, err := client.ListObjects(context.Background(), "test-bucket", "prefix/")
	assert.NoError(t, err)
	assert.Len(t, objects, 2)
}

func TestS3Client_ListObjects_空桶(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	objects, err := client.ListObjects(context.Background(), "test-bucket", "")
	assert.NoError(t, err)
	assert.Empty(t, objects)
}

func TestS3Client_ListObjects_WithMaxObjects(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = map[string][]byte{
		"a.txt": []byte("a"),
		"b.txt": []byte("b"),
		"c.txt": []byte("c"),
	}
	client, server := newTestS3Client(mock)
	defer server.Close()

	objects, err := client.ListObjects(context.Background(), "test-bucket", "",
		objectpkg.WithMaxObjects(2))
	assert.NoError(t, err)
	assert.LessOrEqual(t, len(objects), 2)
}

func TestS3Client_ListObjects_失败(t *testing.T) {
	mock := newMockS3Handler()
	mock.failOp = "list"
	client, server := newTestS3Client(mock)
	defer server.Close()

	_, err := client.ListObjects(context.Background(), "nonexistent-bucket", "")
	assert.Error(t, err)
}
```

- [ ] **Step 2: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/store/object/s3/ -run "TestS3Client"
```

Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/object/s3/s3_test.go && git commit -m "test(store/object/s3): 添加 S3Client 单元测试（httptest 模拟）"
```

---

### Task 7: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/store/object/doc.go`

- [ ] **Step 1: 更新 object/doc.go 文件目录，添加 base_test.go 和 s3 子包**

将 doc.go 中的文件目录部分替换为：

```go
// 文件目录：
//
//	object/
//	├── doc.go         # 包文档
//	├── base.go        # BaseObjectStorage 接口 + ObjectStorageConfig + ListOption
//	├── base_test.go   # 接口与配置的单元测试
//	└── s3/            # S3 兼容后端实现子包
//	    ├── doc.go     # 子包文档
//	    ├── s3.go      # S3Client 实现（NewS3Client + 6 个接口方法）
//	    └── s3_test.go # S3Client 单元测试（httptest 模拟）
```

- [ ] **Step 2: 验证编译和测试通过**

```bash
cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/store/object/...
```

Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/object/doc.go && git commit -m "docs(store/object): 更新 doc.go 文件目录"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 4.27 步骤状态从 ☐ 改为 ✅**

将：
```
| 4.27 | ☐ | Object Store | 对象存储 (OBS/S3) | `openjiuwen/core/foundation/store/object/` |
```
改为：
```
| 4.27 | ✅ | Object Store | 对象存储 (OBS/S3) | `openjiuwen/core/foundation/store/object/` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 4.27 Object Store 为已完成"
```
