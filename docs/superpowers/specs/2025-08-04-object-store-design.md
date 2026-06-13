# Object Store 设计文档

> 日期：2025-08-04
> 对应 Python：`openjiuwen/core/foundation/store/object/`
> 实现位置：`internal/agentcore/store/object/`

## 1. 概述

Object Store 是对象存储的抽象层，提供对 S3 兼容对象存储服务（如华为云 OBS、AWS S3、MinIO 等）的统一访问接口。

它实现了 `BaseObjectStorage` 接口，与已有的 KVStore、VectorStore、GraphStore 等 Store 并列，
为需要对象存储能力的场景（如文件上传/下载、大对象持久化）提供统一能力。

Python 端提供 `BaseObjectStorageClient` 抽象基类和 `AioBotoClient`（基于 aioboto3 的 S3 兼容实现），
Go 端对齐此设计，提供 `BaseObjectStorage` 接口和 `S3Client`（基于 aws-sdk-go-v2 的 S3 兼容实现）。

## 2. 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 后端范围 | 仅 S3 兼容 | 与 Python 端对齐，YAGNI 原则 |
| Go SDK | aws-sdk-go-v2 | 官方新一代 SDK，模块化，可只引入 s3 service 模块，维护活跃 |
| 接口方法 | 严格对齐 Python 6 个 | UploadFile / DownloadFile / DeleteObject / CreateBucket / DeleteBucket / ListObjects |
| ListObjects 可选参数 | Option 函数选项模式 | 与 VectorStore 等已有 Store 风格一致，默认 maxObjects=100 |
| 配置方式 | 结构体 + 环境变量兜底 | 与 Python 端优先级一致：结构体字段 > 环境变量（OBS_SERVER / OBS_ACCESS_KEY_ID / OBS_SECRET_ACCESS_KEY / OBS_REGION） |
| 客户端生命周期 | 长生命周期复用 | aws-sdk-go-v2 的 client 并发安全，底层连接池自动管理，与 Redis/Milvus 等 Store 模式一致 |
| 返回值 | 纯 error，不返回 bool | Go 惯用法，error != nil 即失败 |
| 架构 | 分层子包（object/ + object/s3/） | 与 graph/ 模式一致（graph/base.go + graph/milvus/），未来扩展友好 |
| 单元测试 | httptest 模拟 S3 服务 | 与项目规则 3.3 一致，不依赖外部服务，覆盖率计入基线 |
| 集成测试 | 暂不实现 | 当前阶段不需要，后续按需添加 |

## 3. 核心结构体

### 3.1 BaseObjectStorage 接口

```go
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
```

### 3.2 ObjectStorageConfig 配置

```go
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
```

### 3.3 ListOption 列表查询选项

```go
// ListOption 列表查询选项
type ListOption func(*listOptions)

// listOptions 列表查询内部选项
type listOptions struct {
    maxObjects int
}

// WithMaxObjects 设置最大返回对象数，默认 100
func WithMaxObjects(n int) ListOption {
    return func(o *listOptions) {
        o.maxObjects = n
    }
}
```

### 3.4 S3ClientConfig S3 客户端配置

```go
// S3ClientConfig S3 客户端特定配置
//
// 在 ObjectStorageConfig 基础上增加 S3 SDK 特有的配置项。
// 对应 Python 端 boto3 Config(signature_version="s3v4", s3={"payload_signing_enabled": False})
type S3ClientConfig struct {
    // ObjectStorageConfig 基础对象存储配置
    ObjectStorageConfig
    // SignatureVersion 签名版本，默认 "v4"
    SignatureVersion string
    // PayloadSigningEnabled 是否签名 payload，默认 false
    PayloadSigningEnabled bool
}
```

### 3.5 S3Client 实现

```go
// S3Client 基于 aws-sdk-go-v2 的 S3 兼容对象存储客户端
//
// 支持华为云 OBS 以及任何 S3 兼容的对象存储服务。
// 客户端为长生命周期，并发安全，底层连接池自动管理。
//
// 对应 Python: openjiuwen/core/foundation/store/object/aioboto_storage_client.py
type S3Client struct {
    client *s3.Client
}
```

## 4. 初始化流程

`NewS3Client(cfg S3ClientConfig) (*S3Client, error)` 的初始化步骤：

1. **环境变量兜底**：`cfg.ObjectStorageConfig` 中为空的字段从环境变量读取
   - `Server` 为空 → `os.Getenv("OBS_SERVER")`
   - `AccessKeyID` 为空 → `os.Getenv("OBS_ACCESS_KEY_ID")`
   - `SecretAccessKey` 为空 → `os.Getenv("OBS_SECRET_ACCESS_KEY")`
   - `RegionName` 为空 → `os.Getenv("OBS_REGION")`
2. **构建 AWS 凭证**：使用 `credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")`
3. **构建 AWS 配置**：`config.LoadDefaultConfig()` + 自定义 endpoint resolver（指向 Server URL）
4. **创建 S3 客户端**：设置 endpoint、签名版本 v4、`PayloadSigningEnabled: false`
5. **返回 `*S3Client`**

## 5. 方法实现映射

| 方法 | aws-sdk-go-v2 API | 说明 |
|------|-------------------|------|
| `UploadFile` | `s3.PutObject` | 读取本地文件，通过 `io.Reader` 上传 |
| `DownloadFile` | `s3.GetObject` | 下载到本地文件，通过 `io.Writer` 写入 |
| `DeleteObject` | `s3.DeleteObject` | 直接调用 |
| `CreateBucket` | `s3.CreateBucket` | 传入 `CreateBucketConfiguration` 含 `LocationConstraint` |
| `DeleteBucket` | `s3.DeleteBucket` | 直接调用 |
| `ListObjects` | `s3.ListObjectsV2` | 返回对象列表，解析为 `[]map[string]any` |

## 6. 日志设计

遵循项目日志规范（规则 3），使用 `logger.ComponentCommon` 组件，对齐 Python 端每个方法的 info/error 日志。

### 6.1 成功日志（Info）

| 方法 | 日志内容 | 结构化字段 |
|------|---------|-----------|
| `CreateBucket` | 桶创建成功 | `bucket_name`, `location` |
| `DeleteBucket` | 桶删除成功 | `bucket_name` |
| `UploadFile` | 文件上传成功 | `object_name`, `file_path`, `bucket_name` |
| `DownloadFile` | 文件下载成功 | `object_name`, `bucket_name`, `file_path` |
| `DeleteObject` | 对象删除成功 | `object_name`, `bucket_name` |
| `ListObjects` | 列出对象成功 | `bucket_name`, `object_count` |

### 6.2 失败日志（Error）

所有失败日志包含：
- `event_type=OBJECT_STORE_ERROR`（对象存储操作错误）
- `method` 字段标识当前方法名
- `.Err(err)` 记录原始错误

| 方法 | 日志内容 | 结构化字段 |
|------|---------|-----------|
| `CreateBucket` | 桶创建失败 | `bucket_name`, `location`, `method` |
| `DeleteBucket` | 桶删除失败 | `bucket_name`, `method` |
| `UploadFile` | 文件上传失败 | `object_name`, `bucket_name`, `method` |
| `DownloadFile` | 文件下载失败 | `object_name`, `bucket_name`, `method` |
| `DeleteObject` | 对象删除失败 | `object_name`, `bucket_name`, `method` |
| `ListObjects` | 列出对象失败 | `bucket_name`, `method` |

## 7. 目录结构

```
internal/agentcore/store/object/
├── doc.go              # 包文档
├── base.go             # BaseObjectStorage 接口 + ObjectStorageConfig + ListOption
├── base_test.go        # 接口与配置的单元测试
└── s3/
    ├── doc.go          # 子包文档
    ├── s3.go           # S3Client 实现
    └── s3_test.go      # S3Client 单元测试（httptest 模拟）
```

## 8. 测试设计

### 8.1 object/base_test.go

| 测试用例 | 验证内容 |
|---------|---------|
| `TestWithMaxObjects` | ListOption 设置 maxObjects |
| `TestListOptions_Default` | 不传 Option 时 maxObjects 默认为 100 |
| `TestObjectStorageConfig_EnvFallback` | 环境变量兜底逻辑 |

### 8.2 s3/s3_test.go

使用 `net/http/httptest` 启动本地 HTTP 服务器模拟 S3 API 响应。

| 测试用例 | 验证内容 |
|---------|---------|
| `TestS3Client_UploadFile` | 上传成功 |
| `TestS3Client_UploadFile_失败` | 上传失败时返回 error |
| `TestS3Client_DownloadFile` | 下载成功 |
| `TestS3Client_DownloadFile_文件不存在` | 下载不存在的对象时返回 error |
| `TestS3Client_DeleteObject` | 删除成功 |
| `TestS3Client_DeleteObject_失败` | 删除失败时返回 error |
| `TestS3Client_CreateBucket` | 创建桶成功 |
| `TestS3Client_CreateBucket_失败` | 创建桶失败时返回 error |
| `TestS3Client_DeleteBucket` | 删除桶成功 |
| `TestS3Client_DeleteBucket_失败` | 删除桶失败时返回 error |
| `TestS3Client_ListObjects` | 列出对象成功 |
| `TestS3Client_ListObjects_空桶` | 空桶返回空切片 |
| `TestS3Client_ListObjects_WithMaxObjects` | WithMaxObjects 选项生效 |

## 9. 依赖

| 依赖 | 用途 |
|------|------|
| `github.com/aws/aws-sdk-go-v2` | AWS SDK 核心模块 |
| `github.com/aws/aws-sdk-go-v2/config` | AWS 配置加载 |
| `github.com/aws/aws-sdk-go-v2/credentials` | 静态凭证 |
| `github.com/aws/aws-sdk-go-v2/service/s3` | S3 服务客户端 |
| `github.com/aws/aws-sdk-go-v2/feature/s3/manager` | S3 上传/下载管理器（可选，用于大文件分片） |
