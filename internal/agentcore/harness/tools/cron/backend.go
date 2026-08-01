package cron

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronToolContext 运行时上下文，绑定到 cron 工具注册。
// 对齐 Python: CronToolContext (cron.py L14-26)
type CronToolContext struct {
	// ChannelID 频道标识
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// Metadata 扩展元数据
	Metadata map[string]any
	// Mode 触发模式
	Mode string
}

// ──────────────────────────── 接口 ────────────────────────────

// CronToolBackend 宿主提供的 cron 后端接口，由工具层的通用工厂调用。
// 对齐 Python: CronToolBackend (cron.py L29-88)
type CronToolBackend interface {
	// ListJobs 列出所有 cron 任务。
	// 对齐 Python: list_jobs(*, include_disabled=True)
	ListJobs(ctx context.Context, includeDisabled bool) ([]map[string]any, error)

	// GetJob 根据 ID 获取单个 cron 任务。
	// 对齐 Python: get_job(job_id) -> dict | None
	// Go 中 nil map 表示 Python 的 None
	GetJob(ctx context.Context, jobID string) (map[string]any, error)

	// CreateJob 创建新的 cron 任务。
	// 对齐 Python: create_job(params, *, context=None)
	CreateJob(ctx context.Context, params map[string]any, cronCtx *CronToolContext) (map[string]any, error)

	// UpdateJob 更新已有的 cron 任务。
	// 对齐 Python: update_job(job_id, patch, *, context=None)
	UpdateJob(ctx context.Context, jobID string, patch map[string]any, cronCtx *CronToolContext) (map[string]any, error)

	// DeleteJob 删除 cron 任务。
	// 对齐 Python: delete_job(job_id) -> bool
	DeleteJob(ctx context.Context, jobID string) (bool, error)

	// ToggleJob 启用或禁用 cron 任务。
	// 对齐 Python: toggle_job(job_id, enabled)
	ToggleJob(ctx context.Context, jobID string, enabled bool) (map[string]any, error)

	// PreviewJob 预览下 N 次计划执行时间。
	// 对齐 Python: preview_job(job_id, count=5)
	PreviewJob(ctx context.Context, jobID string, count int) ([]map[string]any, error)

	// RunNow 立即执行一次 cron 任务。
	// 对齐 Python: run_now(job_id) -> str
	RunNow(ctx context.Context, jobID string) (string, error)

	// Status 获取 cron 服务状态。
	// 对齐 Python: status()
	Status(ctx context.Context) (map[string]any, error)

	// GetRuns 获取任务执行记录。
	// 对齐 Python: get_runs(job_id, limit=20)
	GetRuns(ctx context.Context, jobID string, limit int) ([]map[string]any, error)

	// Wake 唤醒，注入提示文本。
	// 对齐 Python: wake(text, *, context=None, mode=None)
	Wake(ctx context.Context, text string, cronCtx *CronToolContext, mode string) (map[string]any, error)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识，为后续 Backend 实现预留
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolScope 计算工具作用域，格式为 "{channel}:{session}"。
// 对齐 Python: CronToolContext.tool_scope (cron.py L23-26)
func (c *CronToolContext) ToolScope() string {
	channel := strings.TrimSpace(c.ChannelID)
	if channel == "" {
		channel = "unknown"
	}
	session := strings.TrimSpace(c.SessionID)
	if session == "" {
		session = "default"
	}
	return channel + ":" + session
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolScope 计算工具作用域并替换冒号为下划线，用于工具名和 agentID。
// 对齐 Python: _tool_scope (cron.py L91-93)
func toolScope(context *CronToolContext) string {
	scope := "cron:default"
	if context != nil {
		scope = context.ToolScope()
	}
	return strings.ReplaceAll(scope, ":", "_")
}
