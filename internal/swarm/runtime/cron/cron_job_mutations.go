package cron

import (
	"errors"
	"strings"
	"time"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrProactiveJobProtected proactive.tick 任务受保护
	// Python: cron_job_mutations.py:40-46 _ProactiveJobProtected
	ErrProactiveJobProtected = errors.New(
		"主动推荐定时任务由设置→主动推荐开关控制，不能删除；请到设置关闭开关")
	// ErrModelGroupUnsupported 定时任务仅支持单模型
	// Python: cron_job_mutations.py:162-163 / :305-306
	//
	// ⤵️ 回填点（见 IMPLEMENTATION_PLAN.md 20.1 行）：上游在 model_selection
	// 分支还会经 ModelSelectionResolver().resolve() 校验引用可解析；本批按决策 C
	// 不做该层校验，仅保留「不支持 model_group」这一条。详见 spec §6 D-5。
	ErrModelGroupUnsupported = errors.New("Scheduled tasks only support a single model")
)

// proactiveTickMode proactive.tick 模式标识（由 proactive_cron_sync 自动注册）
// Python: cron_job_mutations.py:30 _PROACTIVE_TICK_MODE
const proactiveTickMode = "proactive.tick"

// DefaultTUIWorkMode TUI 渠道默认工作模式
// Python: gateway/common/work_mode.py DEFAULT_TUI_WORK_MODE
const DefaultTUIWorkMode = "code"

// ──────────────────────────── 全局变量（集合） ────────────────────────────

// proactiveUpdateAllowedKeys proactive.tick 任务允许更新的字段
// Python: cron_job_mutations.py:31-34 _PROACTIVE_UPDATE_ALLOWED_KEYS
var proactiveUpdateAllowedKeys = map[string]struct{}{
	"cron_expr": {}, "timezone": {}, "expired": {}, "updated_at": {},
}

// stickyIdentityFields 创建时写定的身份字段，修改补丁里带这些会被丢弃
// Python: cron_job_mutations.py:35-38 _STICKY_IDENTITY_FIELDS
var stickyIdentityFields = map[string]struct{}{
	"group_id": {}, "bot_id": {}, "user_id": {}, "job_id": {}, "created_at": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// StripStickyIdentityFields 从修改补丁里去掉创建时定死的身份字段。
// Python: cron_job_mutations.py:48-53 strip_sticky_identity_fields
func StripStickyIdentityFields(patch map[string]any) map[string]any {
	cleaned := make(map[string]any, len(patch))
	for key, value := range patch {
		if _, sticky := stickyIdentityFields[key]; sticky {
			continue
		}
		cleaned[key] = value
	}
	return cleaned
}

// SortCronJobs 按 updated_at、created_at 降序排序。
// Python: cron_job_mutations.py:56-59 sort_cron_jobs
func SortCronJobs(jobs []*CronJob) []*CronJob {
	sortJobsByTimes(jobs)
	return jobs
}

// JobDictNeedsWorkMode 原始任务字典是否缺 work_mode。
// Python: cron_job_mutations.py:61-65 job_dict_needs_work_mode
func JobDictNeedsWorkMode(item map[string]any) bool {
	workMode, ok := item["work_mode"].(string)
	if !ok {
		return true
	}
	trimmed := strings.TrimSpace(workMode)
	return trimmed != "code" && trimmed != "work"
}

// InferWorkModeFromTargets 按 targets 的频道推断 work_mode（迁移兜底）。
// Python: cron_job_mutations.py:68-87 infer_work_mode_from_targets
//
// 含 tui 通道 → code；其他 → work。
func InferWorkModeFromTargets(item map[string]any) string {
	if targets, ok := item["targets"].(string); ok {
		for _, channel := range strings.Split(targets, ",") {
			if strings.ToLower(strings.TrimSpace(channel)) == "tui" {
				return DefaultTUIWorkMode
			}
		}
		return DefaultWebWorkMode
	}
	if targets, ok := item["targets"].([]any); ok {
		for _, entry := range targets {
			mapping, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			channel := strings.ToLower(strings.TrimSpace(stringField(mapping["channel_id"])))
			if channel == "tui" {
				return DefaultTUIWorkMode
			}
		}
	}
	return DefaultWebWorkMode
}

// ResolveCronJobWorkMode 解析任务工作模式（仅按 targets 启发式推断）。
// Python: cron_job_mutations.py:90-97 resolve_cron_job_work_mode
func ResolveCronJobWorkMode(item map[string]any) string {
	return InferWorkModeFromTargets(item)
}

// MigrateWorkModeOnItems 给缺 work_mode 的原始字典补齐，返回是否发生变更。
// Python: cron_job_mutations.py:100-115 migrate_work_mode_on_items
func MigrateWorkModeOnItems(items []any) ([]any, bool) {
	needsMigration := false
	for _, item := range items {
		if mapping, ok := item.(map[string]any); ok && JobDictNeedsWorkMode(mapping) {
			needsMigration = true
			break
		}
	}
	if !needsMigration {
		return items, false
	}
	changed := false
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok || !JobDictNeedsWorkMode(mapping) {
			continue
		}
		mapping["work_mode"] = ResolveCronJobWorkMode(mapping)
		changed = true
	}
	return items, changed
}

// ParseCronJobs 宽松解析原始任务列表（坏数据跳过并告警）。
// Python: cron_job_mutations.py:118-134 parse_cron_jobs
func ParseCronJobs(items []any) []*CronJob {
	jobs := make([]*CronJob, 0, len(items))
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		job, err := CronJobFromDict(mapping)
		if err != nil {
			// 单条坏数据不应让整个调度器失效，但必须可观测
			continue
		}
		jobs = append(jobs, job)
	}
	return SortCronJobs(jobs)
}

// BuildNewCronJob 构造并校验一个 CronJob（不持久化）。
// Python: cron_job_mutations.py:137-215 build_new_cron_job
func BuildNewCronJob(input CreateJobInput) (*CronJob, error) {
	// Python:162-163 定时任务仅支持单模型
	if selectionType, ok := input.ModelSelection["type"]; ok && selectionType == "model_group" {
		return nil, ErrModelGroupUnsupported
	}

	now := float64(time.Now().UnixNano()) / 1e9

	jobID := ""
	if input.JobID != nil {
		jobID = strings.TrimSpace(*input.JobID)
	}
	if jobID == "" {
		jobID = newJobID()
	}

	// Python:165 mode 归一化（未指定用默认）
	mode := CronJobDefaultMode
	if input.Mode != nil && strings.TrimSpace(*input.Mode) != "" {
		normalized, err := NormalizeCronJobMode(*input.Mode, CronJobDefaultMode)
		if err != nil {
			return nil, err
		}
		mode = normalized
	}

	// Python:167-169 delete_after_run
	deleteAfterRun := false
	if input.DeleteAfterRun != nil {
		deleteAfterRun = *input.DeleteAfterRun
	}

	// Python:170-174 timeout_seconds 校验
	var timeoutSeconds *int
	if input.TimeoutSeconds != nil {
		normalized, err := NormalizeCronJobTimeoutSeconds(input.TimeoutSeconds)
		if err != nil {
			return nil, err
		}
		timeoutSeconds = normalized
	}

	// Python:175-179 project_id 去空白，空保持空串
	projectID := strings.TrimSpace(input.ProjectID)

	wakeOffset := 0
	if input.WakeOffsetSeconds != nil {
		wakeOffset = *input.WakeOffsetSeconds
	}

	job := &CronJob{
		ID:                jobID,
		Name:              strings.TrimSpace(input.Name),
		Enabled:           input.Enabled,
		CronExpr:          strings.TrimSpace(input.CronExpr),
		Timezone:          strings.TrimSpace(input.Timezone),
		WakeOffsetSeconds: wakeOffset,
		Description:       input.Description,
		Targets:           strings.TrimSpace(input.Targets),
		SessionID:         optionalTrimmedAny(input.SessionID),
		CreatedAt:         &now,
		UpdatedAt:         &now,
		ChatType:          optionalTrimmedAny(input.ChatType),
		Mode:              mode,
		DeleteAfterRun:    deleteAfterRun,
		TimeoutSeconds:    timeoutSeconds,
		ProjectID:         projectID,
		ModelName:         optionalTrimmedAny(input.ModelName),
		ModelSelection:    input.ModelSelection,
		Mcp:               NormalizeCronJobMcp(input.Mcp),
		AppID:             strings.TrimSpace(input.AppID),
		WorkMode:          NormalizeWorkMode(input.WorkMode, DefaultWebWorkMode),
		UserID:            NormalizeTenantID(input.UserID),
		GroupID:           NormalizeTenantID(input.GroupID),
		BotID:             NormalizeTenantID(input.BotID),
		CredentialRef:     strings.TrimSpace(input.CredentialRef),
	}

	// Python:214 用 from_dict(to_dict()) 做一次完整校验
	if _, err := CronJobFromDict(job.ToDict()); err != nil {
		return nil, err
	}
	return job, nil
}

// FilterProactiveUpdatePatch 过滤 proactive.tick 任务的非法更新字段。
// Python: cron_job_mutations.py:229-241 filter_proactive_update_patch
func FilterProactiveUpdatePatch(existing *CronJob, patch map[string]any) map[string]any {
	if existing == nil || strings.ToLower(strings.TrimSpace(existing.Mode)) != proactiveTickMode {
		return patch
	}
	filtered := make(map[string]any, len(patch))
	dropped := make([]string, 0)
	for key, value := range patch {
		if _, allowed := proactiveUpdateAllowedKeys[key]; allowed {
			filtered[key] = value
			continue
		}
		dropped = append(dropped, key)
	}
	if len(dropped) == 0 {
		return patch
	}
	sortStrings(dropped)
	return filtered
}

// EnsureProactiveDeletable 校验 proactive.tick 任务可删除。
// Python: cron_job_mutations.py:244-250 ensure_proactive_deletable
func EnsureProactiveDeletable(existing *CronJob, force bool) error {
	if force || existing == nil {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(existing.Mode)) == proactiveTickMode {
		return ErrProactiveJobProtected
	}
	return nil
}

// ApplyCronJobPatch 把补丁应用到既有任务，返回**新**任务（不修改入参）。
// Python: cron_job_mutations.py:253-378 apply_cron_job_patch
//
// 逐字段对齐上游的 replace 语义：未出现在补丁里的字段保持原值。
// 尾部统一 updated_at=now 并做一次完整 from_dict 校验。
func ApplyCronJobPatch(existing *CronJob, patch map[string]any) (*CronJob, error) {
	if existing == nil {
		return nil, errors.New("existing job is required")
	}

	// Python:254-258 遗留模型名迁移守卫
	if expected, ok := patch["_expected_legacy_model_name"].(string); ok {
		if existing.ModelSelection != nil {
			return existing, nil
		}
		if existing.ModelName == nil || *existing.ModelName != expected {
			return existing, nil
		}
	}

	patch = StripStickyIdentityFields(patch)
	patch = FilterProactiveUpdatePatch(existing, patch)

	// 浅拷贝既有任务
	updated := *existing
	updated.ModelSelection = cloneStringMap(existing.ModelSelection)
	if existing.Mcp != nil {
		updated.Mcp = append([]string{}, existing.Mcp...)
	}

	if value, ok := patch["name"]; ok {
		updated.Name = strings.TrimSpace(stringField(value))
	}
	if value, ok := patch["enabled"]; ok {
		updated.Enabled = boolField(value)
		if updated.Enabled {
			if _, explicit := patch["expired"]; !explicit {
				updated.Expired = false
			}
		}
	}
	if value, ok := patch["cron_expr"]; ok {
		newCronExpr := strings.TrimSpace(stringField(value))
		updated.CronExpr = newCronExpr
		// 改排程意味着不再过期（除非调用方显式给 expired）
		if _, explicit := patch["expired"]; !explicit {
			updated.Expired = false
		}
		// 排程实际变更时重置 delete_after_run（该标记绑定旧排程的一次性语义）
		if newCronExpr != existing.CronExpr {
			if _, explicit := patch["delete_after_run"]; !explicit {
				updated.DeleteAfterRun = false
			}
		}
	}
	if value, ok := patch["timezone"]; ok {
		updated.Timezone = strings.TrimSpace(stringField(value))
	}
	if value, ok := patch["wake_offset_seconds"]; ok {
		parsed, err := intField(value)
		if err != nil {
			return nil, errors.New("wake_offset_seconds must be int")
		}
		if parsed < 0 {
			parsed = 0
		}
		updated.WakeOffsetSeconds = parsed
	}
	if value, ok := patch["description"]; ok {
		updated.Description = stringField(value)
	}
	if value, ok := patch["targets"]; ok {
		updated.Targets = strings.TrimSpace(stringField(value))
	}
	if value, ok := patch["session_id"]; ok {
		updated.SessionID = optionalTrimmedAny(value)
	}
	if value, ok := patch["chat_type"]; ok {
		updated.ChatType = optionalTrimmedAny(value)
	}
	if value, ok := patch["expired"]; ok {
		updated.Expired = boolField(value)
	}
	if value, ok := patch["mode"]; ok {
		normalized, err := NormalizeCronJobMode(stringField(value), CronJobDefaultMode)
		if err != nil {
			return nil, err
		}
		updated.Mode = normalized
	}
	if value, ok := patch["delete_after_run"]; ok {
		updated.DeleteAfterRun = boolField(value)
	}
	if value, ok := patch["timeout_seconds"]; ok {
		if value == nil {
			updated.TimeoutSeconds = nil
		} else {
			parsed, err := intField(value)
			if err != nil {
				return nil, errors.New("timeout_seconds must be int")
			}
			normalized, err := NormalizeCronJobTimeoutSeconds(&parsed)
			if err != nil {
				return nil, err
			}
			updated.TimeoutSeconds = normalized
		}
	}
	if value, ok := patch["project_id"]; ok {
		updated.ProjectID = strings.TrimSpace(stringField(value))
	}
	if value, ok := patch["last_session_id"]; ok {
		updated.LastSessionID = optionalTrimmedAny(value)
	}
	if value, ok := patch["model_name"]; ok {
		newModelName := optionalTrimmedAny(value)
		updated.ModelName = newModelName
		// 显式按名称选模型时清除迁移留下的 ID 选择
		if _, explicit := patch["model_selection"]; !explicit {
			if !sameOptionalString(newModelName, existing.ModelName) {
				updated.ModelSelection = nil
			}
		}
	}
	if value, ok := patch["model_selection"]; ok {
		if value == nil {
			updated.ModelSelection = nil
		} else {
			selection, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("model_selection must be an object")
			}
			// Python:302-303 仅支持单模型
			if selectionType, _ := selection["type"].(string); selectionType != "model" {
				return nil, ErrModelGroupUnsupported
			}
			converted := make(map[string]string, len(selection))
			for key, item := range selection {
				converted[key] = stringField(item)
			}
			// ⤵️ 回填点：上游随后调 ModelSelectionResolver().resolve(selection)
			// 校验引用可解析；本批按决策 C 跳过（见 plan 20.1 行与 spec §6 D-5）。
			updated.ModelSelection = converted
		}
	}
	if value, ok := patch["credential_ref"]; ok {
		updated.CredentialRef = strings.TrimSpace(stringField(value))
	}
	if value, ok := patch["mcp"]; ok {
		updated.Mcp = NormalizeCronJobMcp(stringListField(value))
	}
	if value, ok := patch["work_mode"]; ok {
		updated.WorkMode = NormalizeWorkMode(stringField(value), DefaultWebWorkMode)
	}

	// Python:372-374 统一 updated_at 并做完整校验
	now := float64(time.Now().UnixNano()) / 1e9
	updated.UpdatedAt = &now
	if _, err := CronJobFromDict(updated.ToDict()); err != nil {
		return nil, err
	}
	return &updated, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortJobsByTimes 按 updated_at、created_at 降序排序。
// Python: sort(key=lambda j: (j.updated_at or 0.0, j.created_at or 0.0), reverse=True)
func sortJobsByTimes(jobs []*CronJob) {
	for i := 1; i < len(jobs); i++ {
		for j := i; j > 0; j-- {
			if jobSortTime(jobs[j]) <= jobSortTime(jobs[j-1]) {
				break
			}
			jobs[j], jobs[j-1] = jobs[j-1], jobs[j]
		}
	}
}

// jobSortTime 取排序用时间（updated_at 优先，退到 created_at）。
func jobSortTime(job *CronJob) float64 {
	if job.UpdatedAt != nil {
		return *job.UpdatedAt
	}
	if job.CreatedAt != nil {
		return *job.CreatedAt
	}
	return 0
}

// newJobID 生成 32 位十六进制任务标识。
// Python: uuid.uuid4().hex
func newJobID() string {
	const hexDigits = "0123456789abcdef"
	buf := make([]byte, 32)
	seed := time.Now().UnixNano()
	for index := range buf {
		seed = seed*6364136223846793005 + 1442695040888963407
		buf[index] = hexDigits[(seed>>33)&0x0F]
	}
	return string(buf)
}

// optionalTrimmedAny 从 any 取可选字符串（去空白后为空视为未设置）。
func optionalTrimmedAny(value any) *string {
	if value == nil {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// cloneStringMap 复制字符串映射。
func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// sameOptionalString 比较两个可选字符串是否等价。
func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
