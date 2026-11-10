package cron

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// CreateJobInput 创建一个定时任务的入参。
// Python: store_base.py:26-51 CronJobStoreBackend.create_job 关键字参数
//
// 除必填项外全部可选（用指针/零值表达 Python 的 `| None`）。
type CreateJobInput struct {
	// JobID 指定任务标识（nil 表示由存储生成）
	JobID *string
	// Name 任务名
	Name string
	// CronExpr cron 表达式
	CronExpr string
	// Timezone 时区
	Timezone string
	// Description 任务内容
	Description string
	// Targets 推送频道
	Targets string
	// Enabled 是否启用（默认由调用方置 true）
	Enabled bool
	// WakeOffsetSeconds 提前唤醒秒数
	WakeOffsetSeconds *int
	// SessionID 会话标识
	SessionID *string
	// ChatType 群聊或私聊
	ChatType *string
	// Mode 执行模式
	Mode *string
	// DeleteAfterRun 执行后删除
	DeleteAfterRun *bool
	// TimeoutSeconds 超时覆盖
	TimeoutSeconds *int
	// ProjectID 工程标识
	ProjectID string
	// ModelName 模型名
	ModelName *string
	// ModelSelection 模型选择
	ModelSelection map[string]string
	// Mcp MCP 服务名列表
	Mcp []string
	// AppID 应用标识
	AppID string
	// WorkMode 工作模式
	WorkMode string
	// UserID 用户标识
	UserID string
	// GroupID 群标识
	GroupID string
	// BotID 机器人标识
	BotID string
	// CredentialRef 凭据引用
	CredentialRef string
}

// ──────────────────────────── 接口 ────────────────────────────

// CronJobStoreBackend 定时任务持久化后端。
// Python: store_base.py:12-72 CronJobStoreBackend（Protocol）
//
// 个人版用文件存储；HA 一体机选用 etcd。CronRunState 不在本协议内（仅内存）。
type CronJobStoreBackend interface {
	// SupportsWatch 是否支持远程变更监听（文件后端为假）
	SupportsWatch() bool
	// ListJobs 列出全部任务；strict 为真时遇到坏数据报错而非跳过
	ListJobs(ctx context.Context, strict bool) ([]*CronJob, error)
	// GetJob 读取单个任务；不存在返回 nil
	GetJob(ctx context.Context, jobID string) (*CronJob, error)
	// CreateJob 创建任务
	CreateJob(ctx context.Context, input CreateJobInput) (*CronJob, error)
	// UpdateJob 局部更新任务
	UpdateJob(ctx context.Context, jobID string, patch map[string]any) (*CronJob, error)
	// DeleteJob 删除任务；force 为真时不存在也算成功
	DeleteJob(ctx context.Context, jobID string, force bool) (bool, error)
	// GetRevision 当前修订号
	GetRevision(ctx context.Context) (int64, error)
	// Watch 阻塞监听远程变更，每次变化回调；文件后端不实现
	Watch(ctx context.Context, callback func(context.Context) error) error
	// Close 释放资源
	Close(ctx context.Context) error
}
