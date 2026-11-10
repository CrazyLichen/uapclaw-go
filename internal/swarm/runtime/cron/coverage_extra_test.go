package cron

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── models 辅助 ────────────────────────────

// TestIntField_各类输入 数字/字符串/布尔/非法。
func TestIntField_各类输入(t *testing.T) {
	got, err := intField(42)
	require.NoError(t, err)
	require.Equal(t, 42, got)

	got, err = intField(int64(7))
	require.NoError(t, err)
	require.Equal(t, 7, got)

	got, err = intField(3.9)
	require.NoError(t, err)
	require.Equal(t, 3, got)

	got, err = intField("13")
	require.NoError(t, err)
	require.Equal(t, 13, got)

	_, err = intField(true)
	require.Error(t, err, "bool 不是 int")

	_, err = intField("abc")
	require.Error(t, err)

	_, err = intField([]string{"x"})
	require.Error(t, err)
}

// TestFloatPointer_各类输入 数值转指针，非数值返回 nil。
func TestFloatPointer_各类输入(t *testing.T) {
	got := floatPointer(1.5)
	require.NotNil(t, got)
	require.Equal(t, 1.5, *got)

	got = floatPointer(2)
	require.NotNil(t, got)
	require.Equal(t, float64(2), *got)

	got = floatPointer(int64(3))
	require.NotNil(t, got)
	require.Equal(t, float64(3), *got)

	require.Nil(t, floatPointer(nil))
	require.Nil(t, floatPointer("1.5"))
	require.Nil(t, floatPointer(true))
}

// TestStringListField_各类输入 数组与字符串切片。
func TestStringListField_各类输入(t *testing.T) {
	require.Equal(t, []string{"a", "b"}, stringListField([]any{"a", "b", 42}))
	require.Equal(t, []string{"x"}, stringListField([]string{"x"}))
	require.Nil(t, stringListField(nil))
	require.Nil(t, stringListField("not-a-list"))
}

// TestOptionalTrimmed_去空白 空白归一为 nil。
func TestOptionalTrimmed_去空白(t *testing.T) {
	got := optionalTrimmed("  x  ")
	require.NotNil(t, got)
	require.Equal(t, "x", *got)

	require.Nil(t, optionalTrimmed("   "))
	require.Nil(t, optionalTrimmed(42))
}

// ──────────────────────────── mutations 辅助 ────────────────────────────

// TestJobSortTime_回退created 无 updated_at 时用 created_at。
func TestJobSortTime_回退created(t *testing.T) {
	created := 100.0
	job := &CronJob{CreatedAt: &created}
	require.Equal(t, 100.0, jobSortTime(job))

	updated := 200.0
	job.UpdatedAt = &updated
	require.Equal(t, 200.0, jobSortTime(job), "updated_at 优先")

	require.Equal(t, 0.0, jobSortTime(&CronJob{}))
	require.Equal(t, 0.0, jobSortTime(nil))
}

// TestOptionalTrimmedAny_各类输入 nil/非字符串/空白。
func TestOptionalTrimmedAny_各类输入(t *testing.T) {
	require.Nil(t, optionalTrimmedAny(nil))
	require.Nil(t, optionalTrimmedAny(42))
	require.Nil(t, optionalTrimmedAny("   "))

	got := optionalTrimmedAny("  x ")
	require.NotNil(t, got)
	require.Equal(t, "x", *got)
}

// TestSameOptionalString_比较 两侧 nil、单侧 nil、相同、不同。
func TestSameOptionalString_比较(t *testing.T) {
	a, b := "x", "y"
	require.True(t, sameOptionalString(nil, nil))
	require.False(t, sameOptionalString(&a, nil))
	require.False(t, sameOptionalString(nil, &a))
	require.True(t, sameOptionalString(&a, &a))
	require.False(t, sameOptionalString(&a, &b))
}

// TestApplyCronJobPatch_废弃模型名守卫 不匹配时原样返回。
// Python: cron_job_mutations.py:254-258
func TestApplyCronJobPatch_废弃模型名守卫(t *testing.T) {
	existing := patchTarget()
	oldName := "old"
	existing.ModelName = &oldName

	// 期望不匹配 → 原样返回
	same, err := ApplyCronJobPatch(existing, map[string]any{"_expected_legacy_model_name": "different"})
	require.NoError(t, err)
	require.Same(t, existing, same)

	// 期望匹配 → 继续应用补丁
	applied, err := ApplyCronJobPatch(existing, map[string]any{
		"_expected_legacy_model_name": "old", "name": "新名",
	})
	require.NoError(t, err)
	require.Equal(t, "新名", applied.Name)
}

// TestApplyCronJobPatch_空任务报错 nil 入参拒绝。
func TestApplyCronJobPatch_空任务报错(t *testing.T) {
	_, err := ApplyCronJobPatch(nil, map[string]any{})
	require.Error(t, err)
}

// TestApplyCronJobPatch_时区与描述 时区、描述、targets 生效。
func TestApplyCronJobPatch_时区与描述(t *testing.T) {
	existing := patchTarget()
	updated, err := ApplyCronJobPatch(existing, map[string]any{
		"timezone": "Europe/London", "description": "新描述", "targets": "tui",
	})
	require.NoError(t, err)
	require.Equal(t, "Europe/London", updated.Timezone)
	require.Equal(t, "新描述", updated.Description)
	require.Equal(t, "tui", updated.Targets)
}

// TestApplyCronJobPatch_超时与MCP 超时设置与清空、MCP 归一。
func TestApplyCronJobPatch_超时与MCP(t *testing.T) {
	existing := patchTarget()
	timeout := 120
	updated, err := ApplyCronJobPatch(existing, map[string]any{
		"timeout_seconds": timeout, "mcp": []any{" a ", "a", "b"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TimeoutSeconds)
	require.Equal(t, 120, *updated.TimeoutSeconds)
	require.Equal(t, []string{"a", "b"}, updated.Mcp)

	cleared, err := ApplyCronJobPatch(updated, map[string]any{"timeout_seconds": nil})
	require.NoError(t, err)
	require.Nil(t, cleared.TimeoutSeconds)
}

// ──────────────────────────── store 分支 ────────────────────────────

// TestStore_upsert保留其他任务 新增只追加，不丢已有任务。
func TestStore_upsert保留其他任务(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)
	second := baseCreateInput()
	second.Name = "第二个"
	secondJob, err := store.CreateJob(ctx, second)
	require.NoError(t, err)

	jobs, err := store.ListJobs(ctx, false)
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	ids := map[string]bool{}
	for _, job := range jobs {
		ids[job.ID] = true
	}
	require.True(t, ids[first.ID])
	require.True(t, ids[secondJob.ID])
}

// TestStore_坏jobs字段返回空 jobs 非数组时读取返回空。
func TestStore_坏jobs字段返回空(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, os.WriteFile(store.Path(), []byte(`{"version":1,"jobs":"not-array"}`), 0o600))

	jobs, err := store.ListJobs(context.Background(), false)
	require.NoError(t, err)
	require.Empty(t, jobs)
}

// TestStore_strict遇非数组报错 strict 下 jobs 非数组上抛。
func TestStore_strict遇非数组报错(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, os.WriteFile(store.Path(), []byte(`{"version":1,"jobs":"not-array"}`), 0o600))

	_, err := store.ListJobs(context.Background(), true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be an array")
}

// TestStore_写盘目录自动创建 深层路径自动建目录。
func TestStore_写盘目录自动创建(t *testing.T) {
	base := t.TempDir()
	store := NewFileCronJobStore(filepath.Join(base, "a", "b", "cron.json"), time.Second)

	_, err := store.CreateJob(context.Background(), baseCreateInput())
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(base, "a", "b", "cron.json"))
	require.NoError(t, statErr, "父目录应自动创建")
}

// TestStoreVersionOf_缺省为1 无 version 字段返回 1。
func TestStoreVersionOf_缺省为1(t *testing.T) {
	require.Equal(t, 1, storeVersionOf(map[string]any{}))
	require.Equal(t, 5, storeVersionOf(map[string]any{"version": 5}))
	require.Equal(t, 1, storeVersionOf(map[string]any{"version": 0}))
}

// TestTrimSpace_去首尾空白 与 strings.TrimSpace 一致。
func TestTrimSpace_去首尾空白(t *testing.T) {
	require.Equal(t, "abc", trimSpace("  abc  "))
	require.Equal(t, "abc", trimSpace("\t\nabc\r\v\f"))
	require.Equal(t, "", trimSpace("   "))
	require.Equal(t, "", trimSpace(""))
}

// ──────────────────────────── factory 分支 ────────────────────────────

// TestParseEtcdEndpoints_字符串切片 []string 输入。
func TestParseEtcdEndpoints_字符串切片(t *testing.T) {
	require.Equal(t, []string{"a", "b"}, parseEtcdEndpoints([]string{" a ", "b", "  "}))
}

// TestParseEtcdEndpoints_空返回nil nil 与空串返回 nil。
func TestParseEtcdEndpoints_空返回nil(t *testing.T) {
	require.Nil(t, parseEtcdEndpoints(nil))
	require.Nil(t, parseEtcdEndpoints(""))
	require.Empty(t, parseEtcdEndpoints("  ,  "), "全空白项被过滤后为空切片")
}
