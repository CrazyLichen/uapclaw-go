package cron

import (
	"errors"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronTarget 推送目标（channel_id + 可选 session_id）。
// Python: models.py:370-387 CronTarget
type CronTarget struct {
	// ChannelID 推送频道标识
	ChannelID string
	// SessionID 会话标识（可选）
	SessionID *string
}

// CronJob 持久化在 cron_jobs.json 中的定时任务。
// Python: models.py:391-610 CronJob（字段定义见 openjiuwen_protocol.cron_models）
//
// 28 个字段与上游逐一对应；可选字段用指针表达 Python 的 `| None`。
type CronJob struct {
	// ID 唯一标识
	ID string
	// Name 任务名（≤ CRON_JOB_NAME_MAX_LENGTH）
	Name string
	// Enabled 是否启用
	Enabled bool
	// Expired 已过期
	Expired bool
	// CronExpr cron 表达式
	CronExpr string
	// Timezone 时区
	Timezone string
	// WakeOffsetSeconds 提前唤醒秒数
	WakeOffsetSeconds int
	// Description 任务内容（≤ CRON_JOB_DESCRIPTION_MAX_LENGTH）
	Description string
	// Targets 推送频道
	Targets string
	// SessionID 会话标识（可选）
	SessionID *string
	// CreatedAt 创建时间（Unix 秒，可选）
	CreatedAt *float64
	// UpdatedAt 更新时间（Unix 秒，可选）
	UpdatedAt *float64
	// ChatType 群聊 group / 私聊 p2p（可选）
	ChatType *string
	// Mode 执行模式
	Mode string
	// DeleteAfterRun 执行一次后删除
	DeleteAfterRun bool
	// TimeoutSeconds 单任务超时覆盖（可选）
	TimeoutSeconds *int
	// ProjectID 工程标识（空串表示默认工程）
	ProjectID string
	// WorkMode 工作模式快照（派生字段）
	WorkMode string
	// LastSessionID 最近使用的会话（可选）
	LastSessionID *string
	// ModelName 模型名（可选）
	ModelName *string
	// ModelSelection 模型选择（可选）
	ModelSelection map[string]string
	// Mcp MCP 服务名列表（可选）
	Mcp []string
	// AppID 应用标识
	AppID string
	// GroupID 群标识（多租户）
	GroupID string
	// BotID 机器人标识（多租户）
	BotID string
	// UserID 用户标识（多租户）
	UserID string
	// CredentialRef 凭据引用
	CredentialRef string
	// NextRunAt 下次触发时间（Unix 秒，可选，不持久化到老数据）
	NextRunAt *float64
}

// CronRunState 单次调度的内存态（不持久化）。
// Python: models.py:620+ CronRunState
type CronRunState struct {
	// JobID 任务标识
	JobID string
	// Running 是否运行中
	Running bool
	// LastRunAt 上次运行时间
	LastRunAt *float64
	// LastResult 上次结果
	LastResult string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CronJobDefaultMode 创建/更新/运行时未指定模式的默认值
	// Python: models.py:117 CRON_JOB_DEFAULT_MODE
	CronJobDefaultMode = "agent"

	// CronJobNameMaxLength 任务名长度上限（与前端 CronTaskDrawer 一致）
	// Python: models.py:122 CRON_JOB_NAME_MAX_LENGTH
	CronJobNameMaxLength = 64

	// CronJobDescriptionMaxLength 描述长度上限
	// Python: models.py:123 CRON_JOB_DESCRIPTION_MAX_LENGTH
	CronJobDescriptionMaxLength = 500

	// CronMaxTimeoutSeconds 单任务超时上限（72 小时）
	// Python: models.py:185 CRON_MAX_TIMEOUT_SECONDS
	CronMaxTimeoutSeconds = 72 * 60 * 60

	// CronMinTimeoutSeconds 单任务超时下限（60 秒）
	// Python: models.py:200 值 < 60 报错
	CronMinTimeoutSeconds = 60

	// CronModelSourceConfig 模型来源：config.yaml 自配
	// Python: models.py:245 CRON_MODEL_SOURCE_CONFIG
	CronModelSourceConfig = "config"
	// CronModelSourceZen 模型来源：Zen 免费缓存
	// Python: models.py:246 CRON_MODEL_SOURCE_ZEN
	CronModelSourceZen = "zen"
	// CronModelSourceLogin 模型来源：登录模型目录
	// Python: models.py:247 CRON_MODEL_SOURCE_LOGIN
	CronModelSourceLogin = "login"

	// DefaultWebWorkMode Web 默认工作模式
	// Python: gateway/common/work_mode.py DEFAULT_WEB_WORK_MODE
	DefaultWebWorkMode = "work"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// CronJobModes 合法的定时任务执行模式集合。
// Python: models.py:82-115 CRON_JOB_MODES
var CronJobModes = map[string]struct{}{
	"agent": {}, "plan": {}, "team": {},
	"agent.plan": {}, "agent.fast": {}, "team.plan": {},
	"team.plan.normal": {}, "team.plan.code": {},
	"code.team": {}, "code": {}, "code.normal": {}, "code.plan": {}, "team.code": {},
	"proactive.tick":    {},
	"agent.work.normal": {}, "agent.work.plan": {},
	"agent.code.normal": {}, "agent.code.plan": {},
	"team.work.normal": {}, "team.work.plan": {},
	"team.code.normal": {}, "team.code.plan": {},
}

// cronJobModeAliases 遗留模式别名 → 折叠目标。
// Python: models.py:125-130 _CRON_JOB_MODE_ALIASES
//
// 注意 agent.plan 是真实 plan 模式，不在此表，交由 schema.CanonicalizeMode 映射。
var cronJobModeAliases = map[string]string{
	"plan":       "agent",
	"agent.fast": "agent",
	"team.plan":  "team.plan.normal",
}

// validTargetChannels 推送频道枚举值。
// Python: openjiuwen_protocol.cron_models.CronTargetChannel
var validTargetChannels = map[string]struct{}{
	"web": {}, "tui": {}, "feishu": {}, "wecom": {},
	"dingtalk": {}, "wechat": {}, "whatsapp": {}, "xiaoyi": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// FeishuEnterpriseAppID 从 feishu_enterprise:<app_id> 形态取 app_id。
// Python: models.py:33-39 _feishu_enterprise_app_id
//
// 忽略 :chat: 等后续后缀；非该前缀返回空串。
func FeishuEnterpriseAppID(raw string) string {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) < 2 {
		return ""
	}
	if strings.ToLower(strings.TrimSpace(parts[0])) != "feishu_enterprise" {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// IsValidTargetChannelID 判断推送频道标识是否合法。
// Python: models.py:41-51 is_valid_target_channel_id
func IsValidTargetChannelID(raw string) bool {
	text := strings.TrimSpace(raw)
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "feishu_enterprise:") {
		return FeishuEnterpriseAppID(text) != ""
	}
	_, ok := validTargetChannels[strings.ToLower(text)]
	return ok
}

// NormalizeTargetChannelID 归一化推送频道标识，非法回退默认值。
// Python: models.py:54-69 normalize_target_channel_id
func NormalizeTargetChannelID(raw string, defaultChannel string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return defaultChannel
	}
	if strings.HasPrefix(text, "feishu_enterprise:") {
		if appID := FeishuEnterpriseAppID(text); appID != "" {
			return "feishu_enterprise:" + appID
		}
		return defaultChannel
	}
	lower := strings.ToLower(text)
	if _, ok := validTargetChannels[lower]; ok {
		return lower
	}
	return defaultChannel
}

// NormalizeCronJobMode 归一化并校验定时任务执行模式（创建/更新 API 用）。
// Python: models.py:126-155 normalize_cron_job_mode
//
// 顺序：缺省/空白 → 默认值；不在合法集合 → 报错；
// 遗留别名先折叠，再经 schema.CanonicalizeMode 映射到当前权威三段模式。
func NormalizeCronJobMode(raw string, defaultMode string) (string, error) {
	if defaultMode == "" {
		defaultMode = CronJobDefaultMode
	}
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return defaultMode, nil
	}
	if _, ok := CronJobModes[value]; !ok {
		return "", fmt.Errorf("invalid cron job mode %q", raw)
	}
	if alias, ok := cronJobModeAliases[value]; ok {
		value = alias
	}
	canonical := schema.CanonicalizeMode(value)
	if canonical == "" {
		return value, nil
	}
	return canonical, nil
}

// CoerceCronJobMode 宽松归一化：非法值回落默认而不报错（老数据读取用）。
// Python: models.py:155-167 coerce_cron_job_mode
func CoerceCronJobMode(raw string, defaultMode string) string {
	mode, err := NormalizeCronJobMode(raw, defaultMode)
	if err != nil {
		if defaultMode == "" {
			return CronJobDefaultMode
		}
		return defaultMode
	}
	return mode
}

// CronJobModesForTools 返回排序后的合法模式列表（供工具描述用）。
// Python: models.py:168-170 cron_job_modes_for_tools
func CronJobModesForTools() []string {
	modes := make([]string, 0, len(CronJobModes))
	for mode := range CronJobModes {
		modes = append(modes, mode)
	}
	sortStrings(modes)
	return modes
}

// NormalizeCronJobTimeoutSeconds 校验单任务超时覆盖（秒）。
// Python: models.py:190-203 normalize_cron_job_timeout_seconds
//
// nil 表示未设置；< 60 或 > 72h 报错。
func NormalizeCronJobTimeoutSeconds(raw *int) (*int, error) {
	if raw == nil {
		return nil, nil
	}
	value := *raw
	if value < CronMinTimeoutSeconds {
		return nil, errors.New("timeout_seconds must be at least 60")
	}
	if value > CronMaxTimeoutSeconds {
		return nil, fmt.Errorf("timeout_seconds must be at most %d", CronMaxTimeoutSeconds)
	}
	return &value, nil
}

// NormalizeCronJobMcp 归一化 MCP 选择（去空白、去空、去重、保序）。
// Python: models.py:205-233 normalize_cron_job_mcp
//
// 只做类型规范化，不校验 MCP 是否存在/连通：断连后旧任务应降级运行而非起不来。
// nil / 空列表统一归一为 nil（与"未选择"同语义）。
func NormalizeCronJobMcp(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if _, duplicated := seen[name]; duplicated {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeTenantID 归一化租户标识（去空白，空保持空串）。
// Python: models.py:357-368 normalize_tenant_id
func NormalizeTenantID(raw string) string {
	return strings.TrimSpace(raw)
}

// NormalizeWorkMode 归一化工作模式，非法回退默认。
// Python: gateway/common/work_mode.py normalize_work_mode
func NormalizeWorkMode(raw string, defaultMode string) string {
	if defaultMode == "" {
		defaultMode = DefaultWebWorkMode
	}
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "work", "code":
		return value
	}
	return defaultMode
}

// IsTeamCronMode 是否为团队模式。
// Python: models.py:352-355 is_team_cron_mode
func IsTeamCronMode(mode string) bool {
	return schema.IsTeamMode(schema.CanonicalizeMode(mode))
}

// CronJobMetadata 定时任务工具元数据。
// Python: models.py:172-183 cron_job_metadata
func CronJobMetadata() map[string]any {
	return map[string]any{
		"modes":                  CronJobModesForTools(),
		"default_mode":           CronJobDefaultMode,
		"name_max_length":        CronJobNameMaxLength,
		"description_max_length": CronJobDescriptionMaxLength,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortStrings 简单插入排序，避免为内部小集合引入 sort 依赖语义差异。
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// ToDict 序列化为 map（E2A payload 兼容）。
// Python: models.py:397-448 CronJob.to_dict
//
// 输出策略逐条对齐：session_id/chat_type/mode/delete_after_run/timeout_seconds/
// last_session_id/model_name/model_selection/mcp/app_id/credential_ref/next_run_at
// 仅在非空时输出；project_id/work_mode/group_id/bot_id/user_id 始终输出。
func (j *CronJob) ToDict() map[string]any {
	enabled := j.Enabled
	expired := j.Expired
	out := map[string]any{
		"id":                  j.ID,
		"name":                j.Name,
		"enabled":             enabled,
		"expired":             expired,
		"cron_expr":           j.CronExpr,
		"timezone":            j.Timezone,
		"wake_offset_seconds": j.WakeOffsetSeconds,
		"description":         j.Description,
		"targets":             j.Targets,
		"created_at":          j.CreatedAt,
		"updated_at":          j.UpdatedAt,
	}
	// Python:411-412 session_id 非空才输出
	if j.SessionID != nil && *j.SessionID != "" {
		out["session_id"] = *j.SessionID
	}
	if j.ChatType != nil && *j.ChatType != "" {
		out["chat_type"] = *j.ChatType
	}
	if j.Mode != "" {
		out["mode"] = j.Mode
	}
	if j.DeleteAfterRun {
		out["delete_after_run"] = true
	}
	if j.TimeoutSeconds != nil {
		out["timeout_seconds"] = *j.TimeoutSeconds
	}
	// Python:425-426 project_id / work_mode 始终输出
	out["project_id"] = j.ProjectID
	workMode := j.WorkMode
	if workMode == "" {
		workMode = DefaultWebWorkMode
	}
	out["work_mode"] = workMode
	if j.LastSessionID != nil && *j.LastSessionID != "" {
		out["last_session_id"] = *j.LastSessionID
	}
	if j.ModelName != nil && *j.ModelName != "" {
		out["model_name"] = *j.ModelName
	}
	if len(j.ModelSelection) > 0 {
		selection := make(map[string]string, len(j.ModelSelection))
		for key, value := range j.ModelSelection {
			selection[key] = value
		}
		out["model_selection"] = selection
	}
	if len(j.Mcp) > 0 {
		out["mcp"] = append([]string{}, j.Mcp...)
	}
	if j.AppID != "" {
		out["app_id"] = j.AppID
	}
	// Python:440-442 三租户字段始终输出（空串也输出）
	out["group_id"] = NormalizeTenantID(j.GroupID)
	out["bot_id"] = NormalizeTenantID(j.BotID)
	out["user_id"] = NormalizeTenantID(j.UserID)
	if j.CredentialRef != "" {
		out["credential_ref"] = j.CredentialRef
	}
	if j.NextRunAt != nil {
		out["next_run_at"] = *j.NextRunAt
	}
	return out
}

// CronJobFromDict 从 map 反序列化 CronJob（严格校验）。
// Python: models.py:450-600 CronJob.from_dict
//
// 校验顺序与上游严格一致：wake_offset → description → id/name → cron_expr →
// timezone → 表达式校验 → targets → 各可选字段归一。
func CronJobFromDict(data map[string]any) (*CronJob, error) {
	if data == nil {
		return nil, errors.New("data is nil")
	}

	jobID := strings.TrimSpace(stringField(data["id"]))
	name := strings.TrimSpace(stringField(data["name"]))
	cronExpr := strings.TrimSpace(stringField(data["cron_expr"]))
	timezone := strings.TrimSpace(stringField(data["timezone"]))
	enabled := boolField(data["enabled"])
	expired := boolField(data["expired"])

	// Python:455-462 wake_offset_seconds 必须是 int，负数钳制为 0
	wakeOffset := 0
	if raw, exists := data["wake_offset_seconds"]; exists && raw != nil {
		parsed, err := intField(raw)
		if err != nil {
			return nil, errors.New("wake_offset_seconds must be int")
		}
		wakeOffset = parsed
	}
	if wakeOffset < 0 {
		wakeOffset = 0
	}

	// Python:464-472 description 必填且不超长
	description := strings.TrimSpace(stringField(data["description"]))
	if description == "" {
		return nil, errors.New("description is required")
	}
	if len([]rune(description)) > CronJobDescriptionMaxLength {
		return nil, fmt.Errorf("description must be at most %d characters", CronJobDescriptionMaxLength)
	}

	// Python:474-484 targets 兼容旧格式 list[dict]
	targetsStr := ""
	switch raw := data["targets"].(type) {
	case string:
		targetsStr = strings.TrimSpace(raw)
	case []any:
		for _, item := range raw {
			if mapping, ok := item.(map[string]any); ok {
				if channel := strings.TrimSpace(stringField(mapping["channel_id"])); channel != "" {
					targetsStr = channel
					break
				}
			}
		}
	}

	// Python:486-497 created_at / updated_at 仅接受数值
	createdAt := floatPointer(data["created_at"])
	updatedAt := floatPointer(data["updated_at"])
	nextRunAt := floatPointer(data["next_run_at"])

	// Python:499-513 必填项与长度校验
	if jobID == "" {
		return nil, errors.New("id is required")
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	if len([]rune(name)) > CronJobNameMaxLength {
		return nil, fmt.Errorf("name must be at most %d characters", CronJobNameMaxLength)
	}
	if cronExpr == "" {
		return nil, errors.New("cron_expr is required")
	}
	if timezone == "" {
		return nil, errors.New("timezone is required")
	}
	if err := ValidateCronExpression(cronExpr, timezone); err != nil {
		return nil, err
	}
	if targetsStr == "" {
		return nil, errors.New("targets is required")
	}
	// Python:515 归一化 targets
	targetsStr = NormalizeTargetChannelID(targetsStr, "web")

	// Python:517-536 可选字符串字段：去空白后为空视为未设置
	jobSessionID := optionalTrimmed(data["session_id"])
	jobChatType := optionalTrimmed(data["chat_type"])
	lastSessionID := optionalTrimmed(data["last_session_id"])
	modelName := optionalTrimmed(data["model_name"])

	// Python:538-540 mode 归一化（老数据用宽松语义）
	jobMode := CoerceCronJobMode(stringField(data["mode"]), CronJobDefaultMode)

	deleteAfterRun := boolField(data["delete_after_run"])

	// Python:544-547 timeout_seconds 可选且校验
	var timeoutSeconds *int
	if raw, exists := data["timeout_seconds"]; exists && raw != nil {
		parsed, err := intField(raw)
		if err != nil {
			return nil, errors.New("timeout_seconds must be int")
		}
		normalized, err := NormalizeCronJobTimeoutSeconds(&parsed)
		if err != nil {
			return nil, err
		}
		timeoutSeconds = normalized
	}

	// Python:549-556 project_id 缺省为空串
	projectID := ""
	if raw, ok := data["project_id"].(string); ok {
		projectID = strings.TrimSpace(raw)
	}

	// Python:558-567 model_selection 仅接受映射
	var modelSelection map[string]string
	if raw, ok := data["model_selection"].(map[string]any); ok {
		modelSelection = make(map[string]string, len(raw))
		for key, value := range raw {
			modelSelection[key] = stringField(value)
		}
	}

	// Python:568-569 mcp 归一化
	mcp := NormalizeCronJobMcp(stringListField(data["mcp"]))

	// Python:570-571 app_id
	appID := ""
	if raw, ok := data["app_id"].(string); ok {
		appID = strings.TrimSpace(raw)
	}

	// Python:573-576 三租户字段
	groupID := NormalizeTenantID(stringField(data["group_id"]))
	botID := NormalizeTenantID(stringField(data["bot_id"]))
	userID := NormalizeTenantID(stringField(data["user_id"]))

	// Python:578-583 credential_ref
	credentialRef := ""
	if raw, ok := data["credential_ref"].(string); ok {
		credentialRef = strings.TrimSpace(raw)
	}

	// Python:585-589 work_mode 只做归一化，不做跨层工程反查
	workMode := NormalizeWorkMode(stringField(data["work_mode"]), DefaultWebWorkMode)

	return &CronJob{
		ID:                jobID,
		Name:              name,
		Enabled:           enabled,
		Expired:           expired,
		CronExpr:          cronExpr,
		Timezone:          timezone,
		WakeOffsetSeconds: wakeOffset,
		Description:       description,
		Targets:           targetsStr,
		SessionID:         jobSessionID,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ChatType:          jobChatType,
		Mode:              jobMode,
		DeleteAfterRun:    deleteAfterRun,
		TimeoutSeconds:    timeoutSeconds,
		ProjectID:         projectID,
		WorkMode:          workMode,
		LastSessionID:     lastSessionID,
		ModelName:         modelName,
		ModelSelection:    modelSelection,
		Mcp:               mcp,
		AppID:             appID,
		GroupID:           groupID,
		BotID:             botID,
		UserID:            userID,
		CredentialRef:     credentialRef,
		NextRunAt:         nextRunAt,
	}, nil
}

// ToDict 序列化推送目标。
// Python: models.py:373-377 CronTarget.to_dict
func (t *CronTarget) ToDict() map[string]any {
	out := map[string]any{"channel_id": t.ChannelID}
	if t.SessionID != nil {
		out["session_id"] = *t.SessionID
	} else {
		out["session_id"] = nil
	}
	return out
}

// CronTargetFromDict 反序列化推送目标（channel_id 必填）。
// Python: models.py:379-387 CronTarget.from_dict
func CronTargetFromDict(data map[string]any) (*CronTarget, error) {
	if data == nil {
		return nil, errors.New("data is nil")
	}
	channelID := strings.TrimSpace(stringField(data["channel_id"]))
	if channelID == "" {
		return nil, errors.New("target.channel_id is required")
	}
	sessionID := optionalTrimmed(data["session_id"])
	return &CronTarget{ChannelID: channelID, SessionID: sessionID}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringField 取字符串字段，非字符串返回空串。
func stringField(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// boolField 取布尔字段，非布尔按假处理。
// Python: bool(data.get("enabled", False))
func boolField(value any) bool {
	flag, ok := value.(bool)
	return ok && flag
}

// intField 解析整数字段（数字或数字字符串，拒绝布尔）。
// Python: int(raw) 语义
func intField(value any) (int, error) {
	switch typed := value.(type) {
	case bool:
		return 0, errors.New("bool is not int")
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		var parsed int
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err != nil {
			return 0, err
		}
		return parsed, nil
	}
	return 0, errors.New("value is not int")
}

// floatPointer 取浮点字段，非数值返回 nil。
// Python: isinstance(raw, (int, float)) → float(raw) else None
func floatPointer(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		return &typed
	case int:
		converted := float64(typed)
		return &converted
	case int64:
		converted := float64(typed)
		return &converted
	}
	return nil
}

// optionalTrimmed 取可选字符串：去空白后为空视为未设置。
// Python: str(raw).strip() if isinstance(raw, str) and str(raw).strip() else None
func optionalTrimmed(value any) *string {
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

// stringListField 取字符串列表，非列表返回 nil。
func stringListField(value any) []string {
	list, ok := value.([]any)
	if !ok {
		if direct, okString := value.([]string); okString {
			return direct
		}
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
