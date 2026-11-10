package cron

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// baseCreateInput 构造最小创建入参。
func baseCreateInput() CreateJobInput {
	return CreateJobInput{
		Name:        "任务",
		CronExpr:    "0 0 9 * * * *",
		Timezone:    "Asia/Shanghai",
		Description: "内容",
		Targets:     "web",
		Enabled:     true,
	}
}

// ──────────────────────────── 身份字段 ────────────────────────────

// TestStripStickyIdentityFields_去掉五个字段 身份字段被丢弃，其余保留。
// Python: cron_job_mutations.py:48-53
func TestStripStickyIdentityFields_去掉五个字段(t *testing.T) {
	patch := map[string]any{
		"name": "新名", "group_id": "g", "bot_id": "b",
		"user_id": "u", "job_id": "j", "created_at": 1.0,
	}
	cleaned := StripStickyIdentityFields(patch)
	require.Len(t, cleaned, 1)
	require.Equal(t, "新名", cleaned["name"])
}

// ──────────────────────────── 工作模式迁移 ────────────────────────────

// TestInferWorkModeFromTargets_tui 含 tui 通道 → code。
// Python: cron_job_mutations.py:68-87
func TestInferWorkModeFromTargets_tui(t *testing.T) {
	require.Equal(t, DefaultTUIWorkMode, InferWorkModeFromTargets(map[string]any{"targets": "tui"}))
	require.Equal(t, DefaultTUIWorkMode, InferWorkModeFromTargets(map[string]any{"targets": "web,tui"}))
	require.Equal(t, DefaultTUIWorkMode, InferWorkModeFromTargets(map[string]any{
		"targets": []any{map[string]any{"channel_id": "TUI"}},
	}))
}

// TestInferWorkModeFromTargets_其他 非 tui → work。
func TestInferWorkModeFromTargets_其他(t *testing.T) {
	require.Equal(t, DefaultWebWorkMode, InferWorkModeFromTargets(map[string]any{"targets": "web"}))
	require.Equal(t, DefaultWebWorkMode, InferWorkModeFromTargets(map[string]any{}))
}

// TestJobDictNeedsWorkMode_判定 缺失/非法为真，code/work 为假。
// Python: cron_job_mutations.py:61-65
func TestJobDictNeedsWorkMode_判定(t *testing.T) {
	require.True(t, JobDictNeedsWorkMode(map[string]any{}))
	require.True(t, JobDictNeedsWorkMode(map[string]any{"work_mode": "bogus"}))
	require.True(t, JobDictNeedsWorkMode(map[string]any{"work_mode": 42}))
	require.False(t, JobDictNeedsWorkMode(map[string]any{"work_mode": "code"}))
	require.False(t, JobDictNeedsWorkMode(map[string]any{"work_mode": " work "}))
}

// TestMigrateWorkModeOnItems_补齐并标记 缺字段被补齐且返回 changed。
// Python: cron_job_mutations.py:100-115
func TestMigrateWorkModeOnItems_补齐并标记(t *testing.T) {
	items := []any{
		map[string]any{"id": "a", "targets": "tui"},
		map[string]any{"id": "b", "targets": "web", "work_mode": "work"},
	}
	migrated, changed := MigrateWorkModeOnItems(items)
	require.True(t, changed)
	require.Equal(t, DefaultTUIWorkMode, migrated[0].(map[string]any)["work_mode"])
	require.Equal(t, "work", migrated[1].(map[string]any)["work_mode"])
}

// TestMigrateWorkModeOnItems_无缺失返回未变更 全部已有则不标记。
func TestMigrateWorkModeOnItems_无缺失返回未变更(t *testing.T) {
	items := []any{map[string]any{"id": "a", "work_mode": "work"}}
	_, changed := MigrateWorkModeOnItems(items)
	require.False(t, changed)
}

// ──────────────────────────── 宽松解析 ────────────────────────────

// TestParseCronJobs_跳过坏数据 坏条目被跳过，好条目保留且已排序。
// Python: cron_job_mutations.py:118-134
func TestParseCronJobs_跳过坏数据(t *testing.T) {
	items := []any{
		map[string]any{"id": "bad"},
		"not-a-map",
		map[string]any{
			"id": "good", "name": "任务", "cron_expr": "0 0 9 * * * *",
			"timezone": "Asia/Shanghai", "description": "内容", "targets": "web",
			"updated_at": 100.0,
		},
	}
	jobs := ParseCronJobs(items)
	require.Len(t, jobs, 1)
	require.Equal(t, "good", jobs[0].ID)
}

// TestSortCronJobs_按更新时间降序 updated_at 新的在前。
// Python: cron_job_mutations.py:56-59
func TestSortCronJobs_按更新时间降序(t *testing.T) {
	older, newer := 100.0, 200.0
	jobs := []*CronJob{
		{ID: "older", UpdatedAt: &older},
		{ID: "newer", UpdatedAt: &newer},
	}
	sorted := SortCronJobs(jobs)
	require.Equal(t, "newer", sorted[0].ID)
}

// ──────────────────────────── 创建 ────────────────────────────

// TestBuildNewCronJob_最小入参 必填齐全可创建，时间戳已填。
// Python: cron_job_mutations.py:137-215
func TestBuildNewCronJob_最小入参(t *testing.T) {
	job, err := BuildNewCronJob(baseCreateInput())
	require.NoError(t, err)
	require.NotEmpty(t, job.ID)
	require.Equal(t, CronJobDefaultMode, job.Mode)
	require.Equal(t, DefaultWebWorkMode, job.WorkMode)
	require.NotNil(t, job.CreatedAt)
	require.NotNil(t, job.UpdatedAt)
}

// TestBuildNewCronJob_指定ID 显式 ID 被采用。
func TestBuildNewCronJob_指定ID(t *testing.T) {
	input := baseCreateInput()
	id := "my-job"
	input.JobID = &id
	job, err := BuildNewCronJob(input)
	require.NoError(t, err)
	require.Equal(t, "my-job", job.ID)
}

// TestBuildNewCronJob_拒绝模型组 定时任务仅支持单模型。
// Python: cron_job_mutations.py:162-163
func TestBuildNewCronJob_拒绝模型组(t *testing.T) {
	input := baseCreateInput()
	input.ModelSelection = map[string]string{"type": "model_group", "id": "g1"}
	_, err := BuildNewCronJob(input)
	require.ErrorIs(t, err, ErrModelGroupUnsupported)
}

// TestBuildNewCronJob_单模型透传 model_selection 为 model 时原样保留。
// ⤵️ 回填点：上游还会校验引用可解析；本批按决策 C 只透传。
func TestBuildNewCronJob_单模型透传(t *testing.T) {
	input := baseCreateInput()
	input.ModelSelection = map[string]string{"type": "model", "id": "m1"}
	job, err := BuildNewCronJob(input)
	require.NoError(t, err)
	require.Equal(t, "m1", job.ModelSelection["id"])
}

// TestBuildNewCronJob_必填缺失报错 名称/表达式缺失时由 from_dict 校验兜住。
func TestBuildNewCronJob_必填缺失报错(t *testing.T) {
	input := baseCreateInput()
	input.Name = ""
	_, err := BuildNewCronJob(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "name is required")
}

// TestBuildNewCronJob_超时越界报错 小于 60 报错。
func TestBuildNewCronJob_超时越界报错(t *testing.T) {
	input := baseCreateInput()
	timeout := 30
	input.TimeoutSeconds = &timeout
	_, err := BuildNewCronJob(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least 60")
}

// TestBuildNewCronJob_mcp归一 MCP 去重去空。
func TestBuildNewCronJob_mcp归一(t *testing.T) {
	input := baseCreateInput()
	input.Mcp = []string{" a ", "a", "", "b"}
	job, err := BuildNewCronJob(input)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, job.Mcp)
}

// ──────────────────────────── proactive 保护 ────────────────────────────

// TestFilterProactiveUpdatePatch_只留允许键 非调度字段被过滤。
// Python: cron_job_mutations.py:229-241
func TestFilterProactiveUpdatePatch_只留允许键(t *testing.T) {
	existing := &CronJob{ID: "p", Mode: proactiveTickMode}
	patch := map[string]any{
		"cron_expr": "0 0 9 * * * *", "timezone": "Asia/Shanghai",
		"name": "改名", "description": "改描述", "enabled": false,
	}
	filtered := FilterProactiveUpdatePatch(existing, patch)
	require.Len(t, filtered, 2)
	require.Contains(t, filtered, "cron_expr")
	require.Contains(t, filtered, "timezone")
	require.NotContains(t, filtered, "name")
}

// TestFilterProactiveUpdatePatch_非proactive原样 普通任务不过滤。
func TestFilterProactiveUpdatePatch_非proactive原样(t *testing.T) {
	existing := &CronJob{ID: "n", Mode: "agent.work.normal"}
	patch := map[string]any{"name": "改名", "enabled": false}
	require.Equal(t, patch, FilterProactiveUpdatePatch(existing, patch))
}

// TestEnsureProactiveDeletable_受保护 proactive 任务不可删。
// Python: cron_job_mutations.py:244-250
func TestEnsureProactiveDeletable_受保护(t *testing.T) {
	existing := &CronJob{ID: "p", Mode: proactiveTickMode}
	require.ErrorIs(t, EnsureProactiveDeletable(existing, false), ErrProactiveJobProtected)
	require.NoError(t, EnsureProactiveDeletable(existing, true), "force 可删")
	require.NoError(t, EnsureProactiveDeletable(nil, false), "不存在不报错")
	require.NoError(t, EnsureProactiveDeletable(&CronJob{Mode: "agent.work.normal"}, false))
}

// ──────────────────────────── 补丁应用 ────────────────────────────

// patchTarget 构造一个可打补丁的任务。
func patchTarget() *CronJob {
	job, _ := BuildNewCronJob(baseCreateInput())
	return job
}

// TestApplyCronJobPatch_改名 只改 name，其余不变。
// Python: cron_job_mutations.py:253-378
func TestApplyCronJobPatch_改名(t *testing.T) {
	existing := patchTarget()
	updated, err := ApplyCronJobPatch(existing, map[string]any{"name": "新名"})
	require.NoError(t, err)
	require.Equal(t, "新名", updated.Name)
	require.Equal(t, existing.CronExpr, updated.CronExpr)
	require.Equal(t, existing.ID, updated.ID)
}

// TestApplyCronJobPatch_不修改入参 返回新对象，原对象不变。
func TestApplyCronJobPatch_不修改入参(t *testing.T) {
	existing := patchTarget()
	originalName := existing.Name
	_, err := ApplyCronJobPatch(existing, map[string]any{"name": "新名"})
	require.NoError(t, err)
	require.Equal(t, originalName, existing.Name)
}

// TestApplyCronJobPatch_启用清除过期 enabled 为真且未显式给 expired 时清过期。
// Python: cron_job_mutations.py:267-271
func TestApplyCronJobPatch_启用清除过期(t *testing.T) {
	existing := patchTarget()
	existing.Expired = true
	updated, err := ApplyCronJobPatch(existing, map[string]any{"enabled": true})
	require.NoError(t, err)
	require.False(t, updated.Expired)
}

// TestApplyCronJobPatch_改排程重置一次性 排程变更时 delete_after_run 复位。
// Python: cron_job_mutations.py:288-292
func TestApplyCronJobPatch_改排程重置一次性(t *testing.T) {
	existing := patchTarget()
	existing.DeleteAfterRun = true
	updated, err := ApplyCronJobPatch(existing, map[string]any{"cron_expr": "0 0 10 * * * *"})
	require.NoError(t, err)
	require.False(t, updated.DeleteAfterRun)
}

// TestApplyCronJobPatch_显式保留一次性 补丁显式给 delete_after_run 时不复位。
func TestApplyCronJobPatch_显式保留一次性(t *testing.T) {
	existing := patchTarget()
	existing.DeleteAfterRun = true
	updated, err := ApplyCronJobPatch(existing, map[string]any{
		"cron_expr": "0 0 10 * * * *", "delete_after_run": true,
	})
	require.NoError(t, err)
	require.True(t, updated.DeleteAfterRun)
}

// TestApplyCronJobPatch_唤醒偏移非整数报错 int 转换失败即错。
// Python: cron_job_mutations.py:278-284
func TestApplyCronJobPatch_唤醒偏移非整数报错(t *testing.T) {
	existing := patchTarget()
	_, err := ApplyCronJobPatch(existing, map[string]any{"wake_offset_seconds": "bad"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "wake_offset_seconds must be int")
}

// TestApplyCronJobPatch_拒绝模型组 补丁里的 model_group 被拒。
// Python: cron_job_mutations.py:305-306
func TestApplyCronJobPatch_拒绝模型组(t *testing.T) {
	existing := patchTarget()
	_, err := ApplyCronJobPatch(existing, map[string]any{
		"model_selection": map[string]any{"type": "model_group", "id": "g1"},
	})
	require.ErrorIs(t, err, ErrModelGroupUnsupported)
}

// TestApplyCronJobPatch_清空模型选择 显式 null 清空。
// Python: cron_job_mutations.py:296-298
func TestApplyCronJobPatch_清空模型选择(t *testing.T) {
	existing := patchTarget()
	existing.ModelSelection = map[string]string{"type": "model", "id": "m1"}
	updated, err := ApplyCronJobPatch(existing, map[string]any{"model_selection": nil})
	require.NoError(t, err)
	require.Nil(t, updated.ModelSelection)
}

// TestApplyCronJobPatch_改模型名清除选择 按名称选模型时清掉 ID 选择。
// Python: cron_job_mutations.py:340-347
func TestApplyCronJobPatch_改模型名清除选择(t *testing.T) {
	existing := patchTarget()
	oldName := "old-model"
	existing.ModelName = &oldName
	existing.ModelSelection = map[string]string{"type": "model", "id": "old-id"}

	updated, err := ApplyCronJobPatch(existing, map[string]any{"model_name": "new-model"})
	require.NoError(t, err)
	require.NotNil(t, updated.ModelName)
	require.Equal(t, "new-model", *updated.ModelName)
	require.Nil(t, updated.ModelSelection, "改模型名应清除旧 ID 选择")
}

// TestApplyCronJobPatch_身份字段被忽略 补丁里的 group_id 等不生效。
// Python: cron_job_mutations.py:260 strip_sticky_identity_fields
func TestApplyCronJobPatch_身份字段被忽略(t *testing.T) {
	existing := patchTarget()
	existing.GroupID = "original-group"
	updated, err := ApplyCronJobPatch(existing, map[string]any{
		"group_id": "hacked-group", "name": "新名",
	})
	require.NoError(t, err)
	require.Equal(t, "original-group", updated.GroupID, "身份字段不可改")
	require.Equal(t, "新名", updated.Name)
}

// TestApplyCronJobPatch_更新时间戳 应用后 updated_at 刷新。
// Python: cron_job_mutations.py:372
func TestApplyCronJobPatch_更新时间戳(t *testing.T) {
	existing := patchTarget()
	before := *existing.UpdatedAt
	updated, err := ApplyCronJobPatch(existing, map[string]any{"name": "新名"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, *updated.UpdatedAt, before)
}

// TestApplyCronJobPatch_名称非法报错 空名由 from_dict 校验兜住。
func TestApplyCronJobPatch_名称非法报错(t *testing.T) {
	existing := patchTarget()
	_, err := ApplyCronJobPatch(existing, map[string]any{"name": ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "name is required")
}
