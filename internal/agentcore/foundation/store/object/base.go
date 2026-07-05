package object

import (
	"context"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

// ListOptions 列表查询选项
type ListOptions struct {
	// MaxObjects 最大返回对象数
	MaxObjects int
}

// ListOption 列表查询选项
type ListOption func(*ListOptions)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// defaultMaxObjects ListObjects 默认最大返回对象数
	defaultMaxObjects = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	// logComponent 对象存储日志组件，agentcore 下的包应使用 ComponentAgentCore
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithMaxObjects 设置最大返回对象数，默认 100
func WithMaxObjects(n int) ListOption {
	return func(o *ListOptions) {
		o.MaxObjects = n
	}
}

// NewListOptions 应用选项并返回默认值
func NewListOptions(opts ...ListOption) ListOptions {
	o := ListOptions{
		MaxObjects: defaultMaxObjects,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

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
