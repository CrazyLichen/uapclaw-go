// Package cron cron 工具运行时桥。
//
// 本文件对应 Python cron_runtime.py:937-984 的 _RestrictedCronBackend：
// 面向 cron 执行会话的 backend 视图——按开关统一拒绝 create/update，
// 防止 cron 运行中再派生新 cron 或改写已有 cron（含自我续期/改期）；
// 其余管理操作照常委托内层 backend。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/cron/cron_runtime.py（_RestrictedCronBackend）
package cronbridge

import (
	"context"
	"errors"

	agentcron "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/cron"
)

// ──────────────────────────── 结构体 ────────────────────────────

// restrictedCronBackend 禁止变更类 cron 操作的 backend 包装。
// Python: _RestrictedCronBackend——create/update 按开关拒绝，其余操作经
// 接口嵌入委托内层 backend（Python __getattr__ 的等价物）。
type restrictedCronBackend struct {
	// inner 内层 backend（其余操作委托）
	agentcron.CronToolBackend
	// allowCreate 是否允许 create_job
	allowCreate bool
	// allowUpdate 是否允许 update_job
	allowUpdate bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// restrictedCreateMessage create 拒绝文案（Python :956-958 逐字节）。
	restrictedCreateMessage = "Creating new cron jobs from a cron session is not allowed; " +
		"manage existing jobs (list/get/delete/toggle/run) instead"
	// restrictedUpdateMessage update 拒绝文案（Python :973-975 逐字节）。
	restrictedUpdateMessage = "Updating cron jobs from a cron session is not allowed; " +
		"manage existing jobs (list/get/delete/toggle/run) instead"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRestrictedCronBackend 包装内层 backend 为受限视图。
// Python: _RestrictedCronBackend.__init__。
func NewRestrictedCronBackend(inner agentcron.CronToolBackend, allowCreate, allowUpdate bool) agentcron.CronToolBackend {
	return &restrictedCronBackend{
		CronToolBackend: inner,
		allowCreate:     allowCreate,
		allowUpdate:     allowUpdate,
	}
}

// IsRestrictedCronBackend 判定 backend 是否受限视图。
// Python: type(backend) is _RestrictedCronBackend（readonly_tool_bindings.py:288）
// 的等价物——Go 无 `type(x) is` 语法，以 reflect 类型相等替代（语言边界偏差，
// 行为等价；登记 spec §7）。
func IsRestrictedCronBackend(backend agentcron.CronToolBackend) bool {
	_, ok := backend.(*restrictedCronBackend)
	return ok
}

// RestrictedInnerBackend 取受限视图的内层 backend（未包装时原样返回）。
// Python: readonly_tool_bindings 的 delegate 挖取（:288-300 校验链）。
func RestrictedInnerBackend(backend agentcron.CronToolBackend) agentcron.CronToolBackend {
	if restricted, ok := backend.(*restrictedCronBackend); ok {
		return restricted.CronToolBackend
	}
	return backend
}

// RestrictedPolicy 取受限视图的开关（未包装时全允许）。
func RestrictedPolicy(backend agentcron.CronToolBackend) (allowCreate, allowUpdate bool, isRestricted bool) {
	if restricted, ok := backend.(*restrictedCronBackend); ok {
		return restricted.allowCreate, restricted.allowUpdate, true
	}
	return true, true, false
}

// ──────────────────────────── 导出方法（覆写两个变更操作） ────────────────

// CreateJob 按开关拒绝或委托。
// Python: create_job（:951-961）——不允许时抛 ValueError，params/context
// 显式丢弃（_ = (params, context)）。
func (r *restrictedCronBackend) CreateJob(ctx context.Context, params map[string]any, cronCtx *agentcron.CronToolContext) (map[string]any, error) {
	if !r.allowCreate {
		_ = params
		_ = cronCtx
		return nil, errors.New(restrictedCreateMessage)
	}
	return r.CronToolBackend.CreateJob(ctx, params, cronCtx)
}

// UpdateJob 按开关拒绝或委托。
// Python: update_job（:963-980）。
func (r *restrictedCronBackend) UpdateJob(ctx context.Context, jobID string, patch map[string]any, cronCtx *agentcron.CronToolContext) (map[string]any, error) {
	if !r.allowUpdate {
		_ = jobID
		_ = patch
		_ = cronCtx
		return nil, errors.New(restrictedUpdateMessage)
	}
	return r.CronToolBackend.UpdateJob(ctx, jobID, patch, cronCtx)
}
