package cron

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 转发层验证 ────────────────────────────
//
// 实体实现与完整用例已迁至 internal/swarm/runtime/cron/models_test.go。
// 本文件只验证兼容转发层与实体一致，避免别名/转调写错。

// TestCronJob_类型别名指向runtime 别名与 runtime 类型完全相同。
func TestCronJob_类型别名指向runtime(t *testing.T) {
	job := &CronJob{ID: "j1", Name: "任务"}
	require.Equal(t, "j1", job.ID)
	// 28 字段：抽查迁移后新增字段可用
	projectID := "p1"
	job.ProjectID = projectID
	require.Equal(t, "p1", job.ProjectID)
}

// TestIsValidTargetChannelID_转发一致 枚举与非法值判定一致。
func TestIsValidTargetChannelID_转发一致(t *testing.T) {
	require.True(t, IsValidTargetChannelID("web"))
	require.True(t, IsValidTargetChannelID("feishu_enterprise:app1"))
	require.False(t, IsValidTargetChannelID("bogus"))
}

// TestNormalizeTargetChannelID_转发一致 非法回退默认。
func TestNormalizeTargetChannelID_转发一致(t *testing.T) {
	require.Equal(t, "web", NormalizeTargetChannelID("bogus", "web"))
	require.Equal(t, "tui", NormalizeTargetChannelID("TUI", "web"))
}

// TestAllTargetChannels_八频道 返回 8 个枚举频道副本。
func TestAllTargetChannels_八频道(t *testing.T) {
	channels := AllTargetChannels()
	require.Len(t, channels, 8)
	require.Equal(t, TargetChannelWeb, channels[0])

	// 修改返回值不影响内部
	channels[0] = "tampered"
	require.Equal(t, TargetChannelWeb, AllTargetChannels()[0])
}

// TestFromCronJobDict_转发一致 必填缺失报错。
func TestFromCronJobDict_转发一致(t *testing.T) {
	_, err := FromCronJobDict(map[string]any{"id": "j1"})
	require.Error(t, err)

	job, err := FromCronJobDict(map[string]any{
		"id": "j1", "name": "任务", "cron_expr": "0 0 9 * * * *",
		"timezone": "Asia/Shanghai", "description": "内容", "targets": "web",
	})
	require.NoError(t, err)
	require.Equal(t, "j1", job.ID)
}

// TestFromCronTargetDict_转发一致 channel_id 必填。
func TestFromCronTargetDict_转发一致(t *testing.T) {
	_, err := FromCronTargetDict(map[string]any{"channel_id": ""})
	require.Error(t, err)

	target, err := FromCronTargetDict(map[string]any{"channel_id": "web"})
	require.NoError(t, err)
	require.Equal(t, "web", target.ChannelID)
}

// TestToDict_转发一致 空任务返回空 map，非空返回字段。
func TestToDict_转发一致(t *testing.T) {
	require.Empty(t, ToDict(nil))

	job := &CronJob{
		ID: "j1", Name: "任务", CronExpr: "0 0 9 * * * *",
		Timezone: "Asia/Shanghai", Description: "内容", Targets: "web",
	}
	got := ToDict(job)
	require.Equal(t, "j1", got["id"])
	// runtime 层 28 字段：project_id/work_mode 始终输出
	require.Contains(t, got, "project_id")
	require.Contains(t, got, "work_mode")
}

// TestCronJobMetadata_五键 gateway 对前端的 cron.job.meta 契约含 5 个键。
// 与 runtime 侧 4 键的差别：额外 default_timeout_seconds /
// default_team_timeout_seconds。
func TestCronJobMetadata_五键(t *testing.T) {
	got := CronJobMetadata()
	require.Len(t, got, 5)
	require.Equal(t, CronJobDefaultMode, got["default_mode"])
	require.Equal(t, 3600, got["default_timeout_seconds"])
	require.Equal(t, 3600, got["default_team_timeout_seconds"])
	require.Equal(t, 72*60*60, got["max_timeout_seconds"])
	modes, ok := got["modes"].([]string)
	require.True(t, ok)
	require.NotEmpty(t, modes)
}
