package cron

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 转发层验证 ────────────────────────────
//
// gateway 侧的 CronJobStore 是同步接口包装，实体持久化逻辑在
// internal/swarm/runtime/cron/store.go（FileCronJobStore）。
// 完整用例见 runtime/cron/store_test.go；本文件验证 gateway 包装层可用。

// newTestGatewayStore 在临时目录创建 gateway 侧 store。
func newTestGatewayStore(t *testing.T) *CronJobStore {
	t.Helper()
	return NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
}

// TestNewCronJobStore_路径可用 显式路径被采用。
func TestNewCronJobStore_路径可用(t *testing.T) {
	store := newTestGatewayStore(t)
	require.NotEmpty(t, store.Path())
	require.True(t, filepath.IsAbs(store.Path()))
}

// TestCreateJob_落盘可读 创建后能列出并读取。
func TestCreateJob_落盘可读(t *testing.T) {
	store := newTestGatewayStore(t)

	created, err := store.CreateJob(CreateJobParams{
		Name: "任务", CronExpr: "0 0 9 * * * *",
		Timezone: "Asia/Shanghai", Description: "内容", Targets: "web",
		Enabled: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	jobs, err := store.ListJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	fetched, err := store.GetJob(created.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	require.Equal(t, "任务", fetched.Name)
}

// TestCreateJob_自动ID ID 为空时自动生成。
func TestCreateJob_自动ID(t *testing.T) {
	store := newTestGatewayStore(t)
	created, err := store.CreateJob(CreateJobParams{
		Name: "任务", CronExpr: "0 0 9 * * * *",
		Timezone: "Asia/Shanghai", Description: "内容", Targets: "web",
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
}

// TestListJobs_空库返回空 无文件时返回空列表。
func TestListJobs_空库返回空(t *testing.T) {
	jobs, err := newTestGatewayStore(t).ListJobs()
	require.NoError(t, err)
	require.Empty(t, jobs)
}

// TestGetJob_不存在返回nil 未知 ID 返回 nil。
func TestGetJob_不存在返回nil(t *testing.T) {
	store := newTestGatewayStore(t)
	job, err := store.GetJob("never")
	require.NoError(t, err)
	require.Nil(t, job)
}

// TestDeleteJob_删除成功 删除后列表为空。
func TestDeleteJob_删除成功(t *testing.T) {
	store := newTestGatewayStore(t)
	created, err := store.CreateJob(CreateJobParams{
		Name: "任务", CronExpr: "0 0 9 * * * *",
		Timezone: "Asia/Shanghai", Description: "内容", Targets: "web",
	})
	require.NoError(t, err)

	deleted, err := store.DeleteJob(created.ID)
	require.NoError(t, err)
	require.True(t, deleted)

	jobs, err := store.ListJobs()
	require.NoError(t, err)
	require.Empty(t, jobs)
}

// TestUpdateJob_改名 补丁生效。
func TestUpdateJob_改名(t *testing.T) {
	store := newTestGatewayStore(t)
	created, err := store.CreateJob(CreateJobParams{
		Name: "任务", CronExpr: "0 0 9 * * * *",
		Timezone: "Asia/Shanghai", Description: "内容", Targets: "web",
	})
	require.NoError(t, err)

	updated, err := store.UpdateJob(created.ID, map[string]any{"name": "新名"})
	require.NoError(t, err)
	require.Equal(t, "新名", updated.Name)
}

// TestCronJobStore_坏文件健壮 坏 JSON 时读路径返回空而非失败。
func TestCronJobStore_坏文件健壮(t *testing.T) {
	store := newTestGatewayStore(t)
	require.NoError(t, os.WriteFile(store.Path(), []byte("{bad json"), 0o600))

	jobs, err := store.ListJobs()
	require.NoError(t, err)
	require.Empty(t, jobs)
}
