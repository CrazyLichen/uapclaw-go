package cron

import (
	runtimecron "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/cron"
)

// ──────────────────────────── 类型别名 ────────────────────────────
//
// 本文件是兼容转发层：cron 模型的实体已归属 Runtime 层
// （上游 jiuwenswarm/gateway/cron/models.py 已退化为 7 行
//  `sys.modules[__name__] = _implementation` 的别名壳）。
// 实体在 internal/swarm/runtime/cron/models.go（CronJob 28 字段），此处保留
// 别名与转调函数，使 gateway 侧的调度器/控制器无需改动引用路径。

// CronJob 定时任务（别名 → runtimecron.CronJob）。
// Python: jiuwenswarm/runtime/cron/models.py:391 CronJob
type CronJob = runtimecron.CronJob

// CronTarget 推送目标（别名 → runtimecron.CronTarget）。
// Python: jiuwenswarm/runtime/cron/models.py:370 CronTarget
type CronTarget = runtimecron.CronTarget

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TargetChannelWeb Web 频道
	TargetChannelWeb = "web"
	// TargetChannelTUI TUI 频道
	TargetChannelTUI = "tui"
	// TargetChannelFeishu 飞书频道
	TargetChannelFeishu = "feishu"
	// TargetChannelFeishuEnterprise 企业飞书前缀（完整形态 feishu_enterprise:<app_id>）
	TargetChannelFeishuEnterprise = "feishu_enterprise"
	// TargetChannelWeCom 企业微信频道
	TargetChannelWeCom = "wecom"
	// TargetChannelDingTalk 钉钉频道
	TargetChannelDingTalk = "dingtalk"
	// TargetChannelWeChat 微信频道
	TargetChannelWeChat = "wechat"
	// TargetChannelWhatsApp WhatsApp 频道
	TargetChannelWhatsApp = "whatsapp"
	// TargetChannelXiaoyi 小艺频道
	TargetChannelXiaoyi = "xiaoyi"

	// CronJobNameMaxLength 任务名长度上限
	// Python: jiuwenswarm/runtime/cron/models.py:122
	CronJobNameMaxLength = runtimecron.CronJobNameMaxLength
	// CronJobDescriptionMaxLength 描述长度上限
	// Python: jiuwenswarm/runtime/cron/models.py:123
	CronJobDescriptionMaxLength = runtimecron.CronJobDescriptionMaxLength
	// CronJobDefaultMode 默认执行模式
	// Python: jiuwenswarm/runtime/cron/models.py:117
	CronJobDefaultMode = runtimecron.CronJobDefaultMode
)

// ──────────────────────────── 全局变量 ────────────────────────────

// allTargetChannels 全部枚举频道（顺序固定，供 UI 下拉）。
var allTargetChannels = []string{
	TargetChannelWeb, TargetChannelTUI, TargetChannelFeishu,
	TargetChannelWeCom, TargetChannelDingTalk, TargetChannelWeChat,
	TargetChannelWhatsApp, TargetChannelXiaoyi,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsValidTargetChannelID 判断推送频道标识是否合法。
// 转发至 runtimecron.IsValidTargetChannelID。
func IsValidTargetChannelID(raw string) bool {
	return runtimecron.IsValidTargetChannelID(raw)
}

// NormalizeTargetChannelID 归一化推送频道标识，非法回退默认值。
// 转发至 runtimecron.NormalizeTargetChannelID。
func NormalizeTargetChannelID(raw string, defaultChannel string) string {
	return runtimecron.NormalizeTargetChannelID(raw, defaultChannel)
}

// AllTargetChannels 返回全部枚举频道（副本）。
func AllTargetChannels() []string {
	out := make([]string, len(allTargetChannels))
	copy(out, allTargetChannels)
	return out
}

// FromCronJobDict 从 map 反序列化 CronJob（严格校验）。
// 转发至 runtimecron.CronJobFromDict。
func FromCronJobDict(data map[string]any) (*CronJob, error) {
	return runtimecron.CronJobFromDict(data)
}

// FromCronTargetDict 从 map 反序列化 CronTarget。
// 转发至 runtimecron.CronTargetFromDict。
func FromCronTargetDict(data map[string]any) (*CronTarget, error) {
	return runtimecron.CronTargetFromDict(data)
}

// CronJobMetadata 定时任务工具元数据（供 TUI / Web 编辑器渲染）。
// Python: jiuwenswarm/runtime/cron/models.py:172-183 cron_job_metadata
//
// 与 runtime 侧 CronJobMetadata 的差别：本函数额外输出
// default_timeout_seconds / default_team_timeout_seconds，
// 属 gateway 对前端 cron.job.meta 的既有契约，保持不变。
func CronJobMetadata() map[string]any {
	return map[string]any{
		"modes":                        append([]string(nil), cronJobModes...),
		"default_mode":                 cronJobDefaultMode,
		"default_timeout_seconds":      cronDefaultTimeoutSeconds,
		"default_team_timeout_seconds": cronTeamDefaultTimeoutSeconds,
		"max_timeout_seconds":          cronMaxTimeoutSeconds,
	}
}

// ToDict 转为 map（E2A payload 兼容）。
// 转发至 runtimecron.CronJob.ToDict。
func ToDict(job *CronJob) map[string]any {
	if job == nil {
		return map[string]any{}
	}
	return job.ToDict()
}
