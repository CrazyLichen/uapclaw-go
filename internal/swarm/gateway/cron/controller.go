package cron

import (
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronController 高层 cron API（WebChannel handlers 用）。
// 对齐 Python: CronController (controller.py L19-229)。
// Go 不做单例（依赖注入即可）；get_tools() 不移植（9.38 agentcore 已实现工具集）。
type CronController struct {
	// store 任务存储
	store *CronJobStore
	// scheduler 调度器
	scheduler *CronSchedulerService
	// targetChannel 默认目标频道（对齐 Python _target_channel）
	targetChannel string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// descriptionTimeKeywords description 中的时间/频率关键词。
// 对齐 Python: _DESCRIPTION_TIME_KEYWORDS。
var descriptionTimeKeywords = []string{"每天", "每周", "每月", "上午", "下午", "早上", "晚上", "凌晨"}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronController 创建控制器。
func NewCronController(store *CronJobStore, scheduler *CronSchedulerService) *CronController {
	return &CronController{store: store, scheduler: scheduler}
}

// SetTargetChannel 设置默认目标频道（对齐 Python set_target_channel）。
func (c *CronController) SetTargetChannel(channel string) {
	c.targetChannel = channel
}

// ListJobs 列出全部任务（map 形态）。
// 对齐 Python: list_jobs (controller.py L119-121)。
func (c *CronController) ListJobs() ([]map[string]any, error) {
	jobs, err := c.store.ListJobs()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.ToDict())
	}
	return out, nil
}

// GetJob 获取单个任务（不存在返回 nil, nil）。
// 对齐 Python: get_job (controller.py L123-125)。
func (c *CronController) GetJob(jobID string) (map[string]any, error) {
	job, err := c.store.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	return job.ToDict(), nil
}

// CreateJob 创建任务（含归一化与校验）。
// 对齐 Python: create_job (controller.py L127-159)。
func (c *CronController) CreateJob(params map[string]any) (map[string]any, error) {
	name := strings.TrimSpace(strAny(params["name"]))
	cronExpr, err := NormalizeCronExpr(strings.TrimSpace(strAny(params["cron_expr"])))
	if err != nil {
		return nil, err
	}
	timezone := strings.TrimSpace(strAny(params["timezone"]))
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	enabled := true
	if v, ok := params["enabled"]; ok {
		enabled = boolAny(v)
	}
	description := strAny(params["description"])
	targets, err := c.normalizeTargets(params["targets"])
	if err != nil {
		return nil, err
	}
	if err := validateSchedule(cronExpr, timezone); err != nil {
		return nil, err
	}
	description = normalizeDescription(description, name)

	routingSID := routingSessionID(targets, params["session_id"])
	chatType := strings.TrimSpace(strAny(params["chat_type"]))
	deleteAfterRun := false
	if v, ok := params["delete_after_run"]; ok {
		deleteAfterRun = boolAny(v)
	}

	var wos *int
	if raw, ok := params["wake_offset_seconds"]; ok && raw != nil {
		n, err := intAny(raw)
		if err != nil {
			return nil, fmt.Errorf("wake_offset_seconds must be int")
		}
		wos = &n
	}

	job, err := c.store.CreateJob(CreateJobParams{
		JobID:            strings.TrimSpace(strAny(params["id"])),
		Name:             name,
		CronExpr:         cronExpr,
		Timezone:         timezone,
		Enabled:          enabled,
		WakeOffsetSeconds: wos,
		Description:      description,
		Targets:          targets,
		SessionID:        routingSID,
		ChatType:         chatType,
		Mode:             strings.TrimSpace(strAny(params["mode"])),
		DeleteAfterRun:   &deleteAfterRun,
	})
	if err != nil {
		return nil, err
	}
	if err := c.scheduler.Reload(); err != nil {
		return nil, err
	}
	return job.ToDict(), nil
}

// UpdateJob 更新任务。
// 对齐 Python: update_job (controller.py L161-190)。
func (c *CronController) UpdateJob(jobID string, patch map[string]any) (map[string]any, error) {
	patch = copyMap(patch)
	if _, ok := patch["targets"]; ok {
		targets, err := c.normalizeTargets(patch["targets"])
		if err != nil {
			return nil, err
		}
		patch["targets"] = targets
	}
	existing, err := c.store.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("job not found")
	}
	if _, ok := patch["cron_expr"]; ok {
		normalized, err := NormalizeCronExpr(strings.TrimSpace(strAny(patch["cron_expr"])))
		if err != nil {
			return nil, err
		}
		patch["cron_expr"] = normalized
	}
	if _, ok := patch["cron_expr"]; ok {
		if _, ok := patch["timezone"]; ok {
			cronExpr := strings.TrimSpace(strAny(patch["cron_expr"]))
			if cronExpr == "" {
				cronExpr = existing.CronExpr
			}
			timezone := strings.TrimSpace(strAny(patch["timezone"]))
			if timezone == "" {
				timezone = existing.Timezone
			}
			if err := validateSchedule(cronExpr, timezone); err != nil {
				return nil, err
			}
		}
	}
	if _, ok := patch["description"]; ok {
		name := strings.TrimSpace(strAny(patch["name"]))
		if name == "" {
			name = existing.Name
		}
		patch["description"] = normalizeDescription(strAny(patch["description"]), name)
	}

	finalTargets := strings.TrimSpace(strAny(patch["targets"]))
	if finalTargets == "" {
		finalTargets = existing.Targets
	}
	if _, ok := patch["session_id"]; ok {
		patch["session_id"] = routingSessionID(finalTargets, patch["session_id"])
	} else if _, ok := patch["targets"]; ok {
		patch["session_id"] = routingSessionID(finalTargets, existing.SessionID)
	}

	job, err := c.store.UpdateJob(jobID, patch)
	if err != nil {
		return nil, err
	}
	if err := c.scheduler.Reload(); err != nil {
		return nil, err
	}
	return job.ToDict(), nil
}

// DeleteJob 删除任务。
// 对齐 Python: delete_job (controller.py L192-196)。
func (c *CronController) DeleteJob(jobID string) (bool, error) {
	deleted, err := c.store.DeleteJob(jobID)
	if err != nil {
		return false, err
	}
	if deleted {
		if err := c.scheduler.Reload(); err != nil {
			return false, err
		}
	}
	return deleted, nil
}

// ToggleJob 启停任务。
// 对齐 Python: toggle_job (controller.py L198-201)。
func (c *CronController) ToggleJob(jobID string, enabled bool) (map[string]any, error) {
	job, err := c.store.UpdateJob(jobID, map[string]any{"enabled": enabled})
	if err != nil {
		return nil, err
	}
	if err := c.scheduler.Reload(); err != nil {
		return nil, err
	}
	return job.ToDict(), nil
}

// PreviewJob 预览下 N 次执行时间。
// 对齐 Python: preview_job (controller.py L203-225)。
func (c *CronController) PreviewJob(jobID string, count int) ([]map[string]any, error) {
	job, err := c.store.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("job not found")
	}
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		return nil, err
	}
	normalized, err := NormalizeCronExpr(job.CronExpr)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	pushDT := time.Now().In(loc)
	for i := 0; i < count; i++ {
		next, err := NextFireTime(normalized, pushDT)
		if err != nil {
			if IsNoNextDateError(err) {
				break
			}
			return nil, err
		}
		pushDT = next
		// 去重（同刻重复触发防御）
		if len(out) > 0 && pushDT.Format(time.RFC3339) == strAny(out[len(out)-1]["push_at"]) {
			break
		}
		wakeDT := next.Add(-time.Duration(max(0, job.WakeOffsetSeconds) * int(time.Second)))
		out = append(out, map[string]any{
			"wake_at": wakeDT.Format(time.RFC3339),
			"push_at": pushDT.Format(time.RFC3339),
		})
	}
	return out, nil
}

// RunNow 立即执行。
// 对齐 Python: run_now (controller.py L227-229)。
func (c *CronController) RunNow(jobID string) (string, error) {
	return c.scheduler.TriggerRunNow(jobID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeTargets 归一化 targets 参数。
// 对齐 Python: _normalize_targets (controller.py L76-88)。
func (c *CronController) normalizeTargets(raw any) (string, error) {
	rawStr := strings.TrimSpace(strAny(raw))
	if c.targetChannel == "" && rawStr == "" {
		return "", fmt.Errorf("targets is required when target_channel is not set")
	}
	if rawStr == "" {
		return NormalizeTargetChannelID(c.targetChannel, TargetChannelWeb), nil
	}
	if !IsValidTargetChannelID(rawStr) {
		return "", fmt.Errorf("targets must be one of tui/web/feishu/dingtalk/whatsapp/wecom/xiaoyi/wechat or feishu_enterprise:<app_id>")
	}
	return NormalizeTargetChannelID(rawStr, TargetChannelWeb), nil
}

// validateSchedule 校验表达式可计算下次触发。
// 对齐 Python: _validate_schedule (controller.py L68-72)。
func validateSchedule(cronExpr, timezone string) error {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return err
	}
	normalized, err := NormalizeCronExpr(cronExpr)
	if err != nil {
		return err
	}
	_, err = NextFireTime(normalized, time.Now().In(loc))
	return err
}

// normalizeDescription description 时间关键词兜底。
// 对齐 Python: _normalize_description (controller.py L91-101)。
func normalizeDescription(description string, name string) string {
	description = strings.TrimSpace(description)
	name = strings.TrimSpace(name)
	if name == "" {
		return description
	}
	hasKeyword := false
	for _, kw := range descriptionTimeKeywords {
		if strings.Contains(description, kw) {
			hasKeyword = true
			break
		}
	}
	if !hasKeyword {
		return description
	}
	if strings.Contains(description, name) || strings.HasSuffix(description, name) {
		return name
	}
	return description
}

// routingSessionID routing session_id 推导。
// 对齐 Python: _routing_session_id (controller.py L103-117)。
// feishu_enterprise 必须是 SessionMap 形态（feishu::...::...），否则空。
func routingSessionID(targets string, raw any) string {
	targetsStr := strings.TrimSpace(targets)
	rawStr := ""
	if s, ok := raw.(string); ok {
		rawStr = strings.TrimSpace(s)
	}
	if rawStr == "" {
		return ""
	}
	if strings.HasPrefix(targetsStr, "feishu_enterprise:") {
		if !strings.Contains(rawStr, "::") {
			return ""
		}
		parts := strings.Split(rawStr, "::")
		if len(parts) < 3 || parts[0] != "feishu" {
			return ""
		}
		return rawStr
	}
	return rawStr
}

// copyMap 浅拷贝 map。
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
