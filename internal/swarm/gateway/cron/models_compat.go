package cron

import (
	"strings"

	runtimecron "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/cron"
)

// ──────────────────────────── 常量 ────────────────────────────
//
// gateway 侧内部常量：与上游 jiuwenswarm/runtime/cron/models.py 同源，
// 因 cron.job.meta 的对外契约含 default_timeout_seconds 等键，
// gateway 的 CronJobMetadata 需保留这 5 个键（runtime 侧只暴露 4 个键）。
// 本文件只为 gateway 内部（controller/scheduler）提供这些常量与取值助手。

const (
	// cronJobDefaultMode 创建/更新未指定 mode 时的默认值
	cronJobDefaultMode = runtimecron.CronJobDefaultMode
	// cronDefaultTimeoutSeconds 默认执行超时（秒）
	// Python: models.py:186 CRON_DEFAULT_TIMEOUT_SECONDS
	cronDefaultTimeoutSeconds = 60 * 60
	// cronTeamDefaultTimeoutSeconds team 模式默认执行超时（秒）
	cronTeamDefaultTimeoutSeconds = 60 * 60
	// cronMaxTimeoutSeconds 单任务超时上限（秒，72 小时）
	cronMaxTimeoutSeconds = runtimecron.CronMaxTimeoutSeconds
)

// ──────────────────────────── 全局变量 ────────────────────────────

// cronJobModes 客户端可选的执行模式集合（已排序，供 cron.job.meta 原样下发）。
// 取自 runtime 层的权威集合，保持单一来源。
var cronJobModes = runtimecron.CronJobModesForTools()

// ──────────────────────────── 非导出函数 ────────────────────────────

// strAny 安全取字符串（nil/非字符串返回空串）。
func strAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// boolAny 按 Python bool() 真值语义转 bool。
// 对齐 Python: bool(data.get("enabled", False))。
func boolAny(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		return val != ""
	case []any:
		return len(val) > 0
	case map[string]any:
		return len(val) > 0
	default:
		return true
	}
}

// floatAny 安全取浮点（非数值回退默认值）。
func floatAny(v any, def float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case bool:
		if n {
			return 1.0
		}
		return 0.0
	}
	return def
}

// modeAny 归一化 mode 字段（空/非字符串默认 "agent"，其余小写化）。
// Python: models.py:203-208
func modeAny(v any) string {
	s := strAny(v)
	if s == "" {
		return cronJobDefaultMode
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// ──────────────────────────── 类型适配助手 ────────────────────────────
//
// runtime 层用指针表达 Python 的 `| None`；gateway 侧的既有签名用空串表达"未设置"。
// 下列助手在两个表示间转换，避免调用方到处写指针样板。

// optionalStringPointer 空串转 nil，非空转指针。
func optionalStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// stringFromOptional 指针转字符串，nil 为空串。
func stringFromOptional(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// jobTime 取任务排序用的 updated_at（nil 记 0）。
func jobTime(job *CronJob) float64 {
	if job == nil || job.UpdatedAt == nil {
		return 0
	}
	return *job.UpdatedAt
}

// jobCreateTime 取任务排序用的 created_at（nil 记 0）。
func jobCreateTime(job *CronJob) float64 {
	if job == nil || job.CreatedAt == nil {
		return 0
	}
	return *job.CreatedAt
}
