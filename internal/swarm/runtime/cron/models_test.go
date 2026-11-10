package cron

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 频道标识 ────────────────────────────

// TestFeishuEnterpriseAppID_取app_id 只认 feishu_enterprise:<app_id> 形态。
// Python: models.py:33-39
func TestFeishuEnterpriseAppID_取app_id(t *testing.T) {
	require.Equal(t, "app123", FeishuEnterpriseAppID("feishu_enterprise:app123"))
	require.Equal(t, "app123", FeishuEnterpriseAppID("feishu_enterprise:app123:chat:c1"))
	require.Equal(t, "", FeishuEnterpriseAppID("feishu_enterprise:"))
	require.Equal(t, "", FeishuEnterpriseAppID("web"))
	require.Equal(t, "", FeishuEnterpriseAppID(""))
}

// TestIsValidTargetChannelID_枚举与前缀 8 个枚举 + 企业飞书前缀。
// Python: models.py:41-51
func TestIsValidTargetChannelID_枚举与前缀(t *testing.T) {
	for _, channel := range []string{"web", "tui", "feishu", "wecom", "dingtalk", "wechat", "whatsapp", "xiaoyi"} {
		require.True(t, IsValidTargetChannelID(channel), channel)
	}
	require.True(t, IsValidTargetChannelID("WEB"), "大小写不敏感")
	require.True(t, IsValidTargetChannelID("feishu_enterprise:app1"))
	require.False(t, IsValidTargetChannelID("feishu_enterprise:"))
	require.False(t, IsValidTargetChannelID("unknown"))
	require.False(t, IsValidTargetChannelID(""))
}

// TestNormalizeTargetChannelID_回退默认 非法回退默认值。
// Python: models.py:54-69
func TestNormalizeTargetChannelID_回退默认(t *testing.T) {
	require.Equal(t, "web", NormalizeTargetChannelID("", "web"))
	require.Equal(t, "tui", NormalizeTargetChannelID("TUI", "web"))
	require.Equal(t, "web", NormalizeTargetChannelID("unknown", "web"))
	require.Equal(t, "feishu_enterprise:app1", NormalizeTargetChannelID("feishu_enterprise:app1:chat:x", "web"))
	require.Equal(t, "web", NormalizeTargetChannelID("feishu_enterprise:", "web"))
}

// ──────────────────────────── 模式归一化 ────────────────────────────

// TestNormalizeCronJobMode_合法模式 全部合法集合成员通过。
// Python: models.py:126-155
func TestNormalizeCronJobMode_合法模式(t *testing.T) {
	for mode := range CronJobModes {
		_, err := NormalizeCronJobMode(mode, "")
		require.NoError(t, err, "mode=%s", mode)
	}
}

// TestNormalizeCronJobMode_别名折叠 plan/agent.fast → agent。
// Python: models.py:125-130 _CRON_JOB_MODE_ALIASES
func TestNormalizeCronJobMode_别名折叠(t *testing.T) {
	got, err := NormalizeCronJobMode("plan", "")
	require.NoError(t, err)
	require.Equal(t, "agent.work.normal", got, "plan → agent → agent.work.normal")

	got, err = NormalizeCronJobMode("agent.fast", "")
	require.NoError(t, err)
	require.Equal(t, "agent.work.normal", got)
}

// TestNormalizeCronJobMode_空用默认 空白回默认值。
func TestNormalizeCronJobMode_空用默认(t *testing.T) {
	got, err := NormalizeCronJobMode("", "")
	require.NoError(t, err)
	require.Equal(t, CronJobDefaultMode, got)

	got, err = NormalizeCronJobMode("   ", "team")
	require.NoError(t, err)
	require.Equal(t, "team", got)
}

// TestNormalizeCronJobMode_非法报错 不在集合内报错。
// Python: models.py:146-149 raise ValueError
func TestNormalizeCronJobMode_非法报错(t *testing.T) {
	_, err := NormalizeCronJobMode("bogus", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cron job mode")
}

// TestCoerceCronJobMode_非法回落 老数据读取不报错。
// Python: models.py:155-167 coerce_cron_job_mode
func TestCoerceCronJobMode_非法回落(t *testing.T) {
	require.Equal(t, CronJobDefaultMode, CoerceCronJobMode("bogus", ""))
	require.Equal(t, "team", CoerceCronJobMode("bogus", "team"))
}

// ──────────────────────────── 超时与 MCP ────────────────────────────

// TestNormalizeCronJobTimeoutSeconds_边界 nil 通过、60 与 72h 通过。
// Python: models.py:190-203
func TestNormalizeCronJobTimeoutSeconds_边界(t *testing.T) {
	got, err := NormalizeCronJobTimeoutSeconds(nil)
	require.NoError(t, err)
	require.Nil(t, got)

	min := 60
	got, err = NormalizeCronJobTimeoutSeconds(&min)
	require.NoError(t, err)
	require.Equal(t, 60, *got)

	maxSeconds := CronMaxTimeoutSeconds
	got, err = NormalizeCronJobTimeoutSeconds(&maxSeconds)
	require.NoError(t, err)
	require.Equal(t, maxSeconds, *got)
}

// TestNormalizeCronJobTimeoutSeconds_越界报错 小于 60 或大于 72h。
func TestNormalizeCronJobTimeoutSeconds_越界报错(t *testing.T) {
	tooSmall := 59
	_, err := NormalizeCronJobTimeoutSeconds(&tooSmall)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least 60")

	tooBig := CronMaxTimeoutSeconds + 1
	_, err = NormalizeCronJobTimeoutSeconds(&tooBig)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most")
}

// TestNormalizeCronJobMcp_去重与去空 保序去重、空列表归一为 nil。
// Python: models.py:205-233
func TestNormalizeCronJobMcp_去重与去空(t *testing.T) {
	require.Nil(t, NormalizeCronJobMcp(nil))
	require.Nil(t, NormalizeCronJobMcp([]string{}))
	require.Nil(t, NormalizeCronJobMcp([]string{"", "   "}))
	require.Equal(t, []string{"b", "a"}, NormalizeCronJobMcp([]string{" b ", "a", "b", ""}), "保序去重：b 先出现")
}

// ──────────────────────────── 序列化 ────────────────────────────

// minimalJob 构造一份最小合法任务。
func minimalJob() *CronJob {
	return &CronJob{
		ID: "job-1", Name: "任务", CronExpr: "0 0 9 * * * *",
		Timezone: "Asia/Shanghai", Description: "内容", Targets: "web",
		Enabled: true, Mode: "agent.work.normal",
	}
}

// TestCronJobToDict_始终输出字段 id/name/enabled 等 11 个 + project_id/work_mode/三租户。
// Python: models.py:397-448
func TestCronJobToDict_始终输出字段(t *testing.T) {
	job := minimalJob()
	got := job.ToDict()

	for _, key := range []string{
		"id", "name", "enabled", "expired", "cron_expr", "timezone",
		"wake_offset_seconds", "description", "targets", "created_at", "updated_at",
	} {
		require.Contains(t, got, key)
	}
	require.Equal(t, "", got["project_id"], "project_id 始终输出")
	require.Equal(t, DefaultWebWorkMode, got["work_mode"], "work_mode 始终输出默认值")
	require.Equal(t, "", got["group_id"], "三租户字段始终输出")
	require.Equal(t, "", got["bot_id"])
	require.Equal(t, "", got["user_id"])
}

// TestCronJobToDict_可选字段非空才输出 session_id 等空值时不出现。
// Python: models.py:411-448
func TestCronJobToDict_可选字段非空才输出(t *testing.T) {
	job := minimalJob()
	got := job.ToDict()
	for _, key := range []string{
		"session_id", "chat_type", "delete_after_run", "timeout_seconds",
		"last_session_id", "model_name", "model_selection", "mcp",
		"app_id", "credential_ref", "next_run_at",
	} {
		require.NotContains(t, got, key, "空值不应输出 %s", key)
	}
}

// TestCronJobToDict_可选字段有值就输出 全字段赋值后逐一出现。
// Python: models.py:411-448
func TestCronJobToDict_可选字段有值就输出(t *testing.T) {
	sessionID := "s1"
	chatType := "group"
	lastSession := "s2"
	modelName := "m1"
	timeout := 120
	nextRun := 1700000000.0
	job := minimalJob()
	job.SessionID = &sessionID
	job.ChatType = &chatType
	job.LastSessionID = &lastSession
	job.ModelName = &modelName
	job.TimeoutSeconds = &timeout
	job.NextRunAt = &nextRun
	job.DeleteAfterRun = true
	job.ModelSelection = map[string]string{"provider": "p"}
	job.Mcp = []string{"mcp-a"}
	job.AppID = "app"
	job.CredentialRef = "cred"
	job.ProjectID = "proj"
	job.GroupID = "g"
	job.BotID = "b"
	job.UserID = "u"

	got := job.ToDict()
	require.Equal(t, "s1", got["session_id"])
	require.Equal(t, "group", got["chat_type"])
	require.Equal(t, "s2", got["last_session_id"])
	require.Equal(t, "m1", got["model_name"])
	require.Equal(t, 120, got["timeout_seconds"])
	require.Equal(t, nextRun, got["next_run_at"])
	require.Equal(t, true, got["delete_after_run"])
	require.Equal(t, map[string]string{"provider": "p"}, got["model_selection"])
	require.Equal(t, []string{"mcp-a"}, got["mcp"])
	require.Equal(t, "app", got["app_id"])
	require.Equal(t, "cred", got["credential_ref"])
	require.Equal(t, "proj", got["project_id"])
	require.Equal(t, "g", got["group_id"])
}

// ──────────────────────────── 反序列化校验 ────────────────────────────

// TestCronJobFromDict_最小数据 必填齐全可解析。
// Python: models.py:450-600
func TestCronJobFromDict_最小数据(t *testing.T) {
	job, err := CronJobFromDict(map[string]any{
		"id": "job-1", "name": "任务", "cron_expr": "0 0 9 * * * *",
		"timezone": "Asia/Shanghai", "description": "内容", "targets": "web",
	})
	require.NoError(t, err)
	require.Equal(t, "job-1", job.ID)
	require.Equal(t, "web", job.Targets)
	require.Equal(t, CronJobDefaultMode, job.Mode)
	require.Equal(t, DefaultWebWorkMode, job.WorkMode)
}

// TestCronJobFromDict_缺必填报错 逐项缺失报错。
func TestCronJobFromDict_缺必填报错(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"id": "job-1", "name": "任务", "cron_expr": "0 0 9 * * * *",
			"timezone": "Asia/Shanghai", "description": "内容", "targets": "web",
		}
	}
	cases := map[string]string{
		"id": "id is required", "name": "name is required",
		"cron_expr": "cron_expr is required", "timezone": "timezone is required",
		"description": "description is required", "targets": "targets is required",
	}
	for field, wantMessage := range cases {
		data := base()
		delete(data, field)
		_, err := CronJobFromDict(data)
		require.Error(t, err, "field=%s", field)
		require.Contains(t, err.Error(), wantMessage, "field=%s", field)
	}
}

// TestCronJobFromDict_名称超长报错 超过 64 字符。
// Python: models.py:506-510
func TestCronJobFromDict_名称超长报错(t *testing.T) {
	long := make([]rune, CronJobNameMaxLength+1)
	for index := range long {
		long[index] = 'a'
	}
	_, err := CronJobFromDict(map[string]any{
		"id": "job-1", "name": string(long), "cron_expr": "0 0 9 * * * *",
		"timezone": "Asia/Shanghai", "description": "内容", "targets": "web",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 64")
}

// TestCronJobFromDict_描述超长报错 超过 500 字符。
// Python: models.py:468-472
func TestCronJobFromDict_描述超长报错(t *testing.T) {
	long := make([]rune, CronJobDescriptionMaxLength+1)
	for index := range long {
		long[index] = 'a'
	}
	_, err := CronJobFromDict(map[string]any{
		"id": "job-1", "name": "任务", "cron_expr": "0 0 9 * * * *",
		"timezone": "Asia/Shanghai", "description": string(long), "targets": "web",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 500")
}

// TestCronJobFromDict_唤醒偏移非整数报错 int 转换失败即错。
// Python: models.py:456-462
func TestCronJobFromDict_唤醒偏移非整数报错(t *testing.T) {
	_, err := CronJobFromDict(map[string]any{
		"id": "job-1", "name": "任务", "cron_expr": "0 0 9 * * * *",
		"timezone": "Asia/Shanghai", "description": "内容", "targets": "web",
		"wake_offset_seconds": "not-a-number",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "wake_offset_seconds must be int")
}

// TestCronJobFromDict_唤醒偏移负数钳制 负数归零。
func TestCronJobFromDict_唤醒偏移负数钳制(t *testing.T) {
	job, err := CronJobFromDict(map[string]any{
		"id": "job-1", "name": "任务", "cron_expr": "0 0 9 * * * *",
		"timezone": "Asia/Shanghai", "description": "内容", "targets": "web",
		"wake_offset_seconds": -50,
	})
	require.NoError(t, err)
	require.Equal(t, 0, job.WakeOffsetSeconds)
}

// TestCronJobFromDict_旧targets格式兼容 list[dict] 取首个非空 channel_id。
// Python: models.py:474-484
func TestCronJobFromDict_旧targets格式兼容(t *testing.T) {
	job, err := CronJobFromDict(map[string]any{
		"id": "job-1", "name": "任务", "cron_expr": "0 0 9 * * * *",
		"timezone": "Asia/Shanghai", "description": "内容",
		"targets": []any{
			map[string]any{"channel_id": ""},
			map[string]any{"channel_id": "tui"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "tui", job.Targets)
}

// TestCronJobFromDict_往返一致 ToDict 后再 FromDict 得到等价字段。
func TestCronJobFromDict_往返一致(t *testing.T) {
	original := minimalJob()
	original.ProjectID = "proj"
	original.GroupID = "g"
	original.UserID = "u"

	parsed, err := CronJobFromDict(original.ToDict())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)
	require.Equal(t, original.Name, parsed.Name)
	require.Equal(t, original.CronExpr, parsed.CronExpr)
	require.Equal(t, original.ProjectID, parsed.ProjectID)
	require.Equal(t, original.GroupID, parsed.GroupID)
	require.Equal(t, original.UserID, parsed.UserID)
}

// TestCronTargetToDict与FromDict 往返一致，channel_id 必填。
// Python: models.py:373-387
func TestCronTargetToDict与FromDict(t *testing.T) {
	sessionID := "s1"
	target := &CronTarget{ChannelID: "web", SessionID: &sessionID}
	dict := target.ToDict()
	require.Equal(t, "web", dict["channel_id"])
	require.Equal(t, "s1", dict["session_id"])

	parsed, err := CronTargetFromDict(dict)
	require.NoError(t, err)
	require.Equal(t, "web", parsed.ChannelID)
	require.NotNil(t, parsed.SessionID)
	require.Equal(t, "s1", *parsed.SessionID)

	_, err = CronTargetFromDict(map[string]any{"channel_id": ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "target.channel_id is required")
}

// ──────────────────────────── 元数据 ────────────────────────────

// TestCronJobMetadata_四字段 含模式列表与长度上限。
// Python: models.py:172-183
func TestCronJobMetadata_四字段(t *testing.T) {
	got := CronJobMetadata()
	require.Len(t, got, 4)
	require.Equal(t, CronJobDefaultMode, got["default_mode"])
	require.Equal(t, CronJobNameMaxLength, got["name_max_length"])
	require.Equal(t, CronJobDescriptionMaxLength, got["description_max_length"])
	modes, ok := got["modes"].([]string)
	require.True(t, ok)
	require.NotEmpty(t, modes)
}

// TestNormalizeTenantID_去空白 空白归空串。
// Python: models.py:357-368
func TestNormalizeTenantID_去空白(t *testing.T) {
	require.Equal(t, "g1", NormalizeTenantID("  g1  "))
	require.Equal(t, "", NormalizeTenantID("   "))
}

// TestIsTeamCronMode_团队模式 team 族为真。
// Python: models.py:352-355
func TestIsTeamCronMode_团队模式(t *testing.T) {
	require.True(t, IsTeamCronMode("team"))
	require.True(t, IsTeamCronMode("team.work.normal"))
	require.False(t, IsTeamCronMode("agent.work.normal"))
}
