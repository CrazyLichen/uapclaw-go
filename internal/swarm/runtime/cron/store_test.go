package cron

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTestStore 在临时目录创建文件存储。
func newTestStore(t *testing.T) *FileCronJobStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cron_jobs.json")
	return NewFileCronJobStore(path, 5*time.Second)
}

// ──────────────────────────── 基本属性 ────────────────────────────

// TestFileCronJobStore_不支持watch 文件后端 supports_watch 为假。
// Python: store.py:49 supports_watch = False
func TestFileCronJobStore_不支持watch(t *testing.T) {
	store := newTestStore(t)
	require.False(t, store.SupportsWatch())
	require.ErrorIs(t, store.Watch(context.Background(), nil), ErrWatchUnsupported)
}

// TestFileCronJobStore_路径可读 构造后 Path 返回同一路径。
// Python: store.py:57-59
func TestFileCronJobStore_路径可读(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.json")
	store := NewFileCronJobStore(path, time.Second)
	require.Equal(t, path, store.Path())
}

// TestFileCronJobStore_关闭无操作 关闭不报错。
// Python: store.py:383-384
func TestFileCronJobStore_关闭无操作(t *testing.T) {
	require.NoError(t, newTestStore(t).Close(context.Background()))
}

// ──────────────────────────── 空文件 ────────────────────────────

// TestListJobs_文件不存在返回空 无文件时返回空列表而非错误。
// Python: store.py:341-363 缺文件返回 {"version":1,"jobs":[]}
func TestListJobs_文件不存在返回空(t *testing.T) {
	jobs, err := newTestStore(t).ListJobs(context.Background(), false)
	require.NoError(t, err)
	require.Empty(t, jobs)
}

// TestListJobs_坏JSON返回空 非 strict 下坏文件退回空结构。
func TestListJobs_坏JSON返回空(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, os.WriteFile(store.Path(), []byte("{not json"), 0o600))

	jobs, err := store.ListJobs(context.Background(), false)
	require.NoError(t, err)
	require.Empty(t, jobs)
}

// ──────────────────────────── 创建与读取 ────────────────────────────

// TestCreateJob_落盘且可读 创建后 list/get 都能拿到。
// Python: store.py:183-231 create_job
func TestCreateJob_落盘且可读(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	jobs, err := store.ListJobs(ctx, false)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, created.ID, jobs[0].ID)

	fetched, err := store.GetJob(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	require.Equal(t, created.Name, fetched.Name)
}

// TestCreateJob_两次创建累积 第二个任务追加而非覆盖。
func TestCreateJob_两次创建累积(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)
	second := baseCreateInput()
	second.Name = "第二个"
	_, err = store.CreateJob(ctx, second)
	require.NoError(t, err)

	jobs, err := store.ListJobs(ctx, false)
	require.NoError(t, err)
	require.Len(t, jobs, 2)
}

// TestCreateJob_非法入参不落盘 校验失败时文件不产生。
// Python: :202-206 build_job 先校验再 upsert
func TestCreateJob_非法入参不落盘(t *testing.T) {
	store := newTestStore(t)
	input := baseCreateInput()
	input.Name = ""

	_, err := store.CreateJob(context.Background(), input)
	require.Error(t, err)

	jobs, err := store.ListJobs(context.Background(), false)
	require.NoError(t, err)
	require.Empty(t, jobs)
}

// TestGetJob_不存在返回nil 空 ID 与未知 ID 都返回 nil。
// Python: store.py:117-125
func TestGetJob_不存在返回nil(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job, err := store.GetJob(ctx, "")
	require.NoError(t, err)
	require.Nil(t, job)

	job, err = store.GetJob(ctx, "never-exists")
	require.NoError(t, err)
	require.Nil(t, job)
}

// ──────────────────────────── 更新 ────────────────────────────

// TestUpdateJob_改名 补丁生效且落盘。
// Python: store.py:233-272
func TestUpdateJob_改名(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)

	updated, err := store.UpdateJob(ctx, created.ID, map[string]any{"name": "新名"})
	require.NoError(t, err)
	require.Equal(t, "新名", updated.Name)

	fetched, err := store.GetJob(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "新名", fetched.Name)
}

// TestUpdateJob_不存在报错 未知 ID 返回 ErrJobNotFound。
// Python: raise KeyError("job not found")
func TestUpdateJob_不存在报错(t *testing.T) {
	store := newTestStore(t)
	_, err := store.UpdateJob(context.Background(), "never", map[string]any{"name": "x"})
	require.ErrorIs(t, err, ErrJobNotFound)
}

// TestUpdateJob_空ID报错 空 ID 直接报错。
// Python: store.py:235-236 raise ValueError("id is required")
func TestUpdateJob_空ID报错(t *testing.T) {
	store := newTestStore(t)
	_, err := store.UpdateJob(context.Background(), "", map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "id is required")
}

// TestUpdateJob_只改补丁字段 未在补丁里的字段保持不变。
func TestUpdateJob_只改补丁字段(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)
	originalExpr := created.CronExpr
	originalTargets := created.Targets

	updated, err := store.UpdateJob(ctx, created.ID, map[string]any{"name": "新名"})
	require.NoError(t, err)
	require.Equal(t, originalExpr, updated.CronExpr)
	require.Equal(t, originalTargets, updated.Targets)
}

// ──────────────────────────── 删除 ────────────────────────────

// TestDeleteJob_删除成功 删除返回真且列表变空。
// Python: store.py:274-300
func TestDeleteJob_删除成功(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)

	deleted, err := store.DeleteJob(ctx, created.ID, false)
	require.NoError(t, err)
	require.True(t, deleted)

	jobs, err := store.ListJobs(ctx, false)
	require.NoError(t, err)
	require.Empty(t, jobs)
}

// TestDeleteJob_不存在返回false 未知 ID 不报错。
func TestDeleteJob_不存在返回false(t *testing.T) {
	store := newTestStore(t)
	deleted, err := store.DeleteJob(context.Background(), "never", false)
	require.NoError(t, err)
	require.False(t, deleted)
}

// TestDeleteJob_空ID返回false 空 ID 直接返回假。
func TestDeleteJob_空ID返回false(t *testing.T) {
	store := newTestStore(t)
	deleted, err := store.DeleteJob(context.Background(), "", false)
	require.NoError(t, err)
	require.False(t, deleted)
}

// TestDeleteJob_proactive受保护 未 force 时拒删。
// Python: store.py:280-281 ensure_proactive_deletable
func TestDeleteJob_proactive受保护(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	input := baseCreateInput()
	mode := proactiveTickMode
	input.Mode = &mode
	input.Targets = "web"
	created, err := store.CreateJob(ctx, input)
	require.NoError(t, err)

	_, err = store.DeleteJob(ctx, created.ID, false)
	require.ErrorIs(t, err, ErrProactiveJobProtected)

	deleted, err := store.DeleteJob(ctx, created.ID, true)
	require.NoError(t, err)
	require.True(t, deleted, "force 可删")
}

// ──────────────────────────── 工程停用 ────────────────────────────

// TestDisableProjectJobs_停用匹配任务 同工程任务被停用。
// Python: store.py:302-312
func TestDisableProjectJobs_停用匹配任务(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	input := baseCreateInput()
	input.ProjectID = "proj-a"
	created, err := store.CreateJob(ctx, input)
	require.NoError(t, err)

	require.NoError(t, store.DisableProjectJobs(ctx, "proj-a"))

	fetched, err := store.GetJob(ctx, created.ID)
	require.NoError(t, err)
	require.False(t, fetched.Enabled)
}

// TestDisableProjectJobs_不匹配不动 其他工程任务保持启用。
func TestDisableProjectJobs_不匹配不动(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	input := baseCreateInput()
	input.ProjectID = "proj-a"
	created, err := store.CreateJob(ctx, input)
	require.NoError(t, err)

	require.NoError(t, store.DisableProjectJobs(ctx, "proj-b"))

	fetched, err := store.GetJob(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, fetched.Enabled)
}

// ──────────────────────────── 修订号与迁移 ────────────────────────────

// TestGetRevision_随写入变化 写入后修订号改变。
// Python: store.py:71-85
func TestGetRevision_随写入变化(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	before, err := store.GetRevision(ctx)
	require.NoError(t, err)

	_, err = store.CreateJob(ctx, baseCreateInput())
	require.NoError(t, err)

	after, err := store.GetRevision(ctx)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}

// TestListJobs_惰性迁移workMode 老数据缺 work_mode 时按 targets 补齐。
// Python: store.py:99-112 migrate_work_mode_on_items
func TestListJobs_惰性迁移workMode(t *testing.T) {
	store := newTestStore(t)
	// 手写一份缺 work_mode 的老数据
	legacy := `{"version":1,"jobs":[{"id":"old","name":"老任务","cron_expr":"0 0 9 * * * *","timezone":"Asia/Shanghai","description":"内容","targets":"tui"}]}`
	require.NoError(t, os.WriteFile(store.Path(), []byte(legacy), 0o600))

	jobs, err := store.ListJobs(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, DefaultTUIWorkMode, jobs[0].WorkMode, "tui 目标迁移为 code")
}

// TestListJobs_strict遇坏数据报错 strict 模式不跳过坏条目。
// Python: store.py:110-113 strict 抛错
func TestListJobs_strict遇坏数据报错(t *testing.T) {
	store := newTestStore(t)
	legacy := `{"version":1,"jobs":[{"id":"only-id"}]}`
	require.NoError(t, os.WriteFile(store.Path(), []byte(legacy), 0o600))

	_, err := store.ListJobs(context.Background(), true)
	require.Error(t, err)
}

// TestBuildJob_委托构造 build_job 等价 BuildNewCronJob。
// Python: store.py:120-181
func TestBuildJob_委托构造(t *testing.T) {
	store := newTestStore(t)
	job, err := store.BuildJob(baseCreateInput())
	require.NoError(t, err)
	require.NotEmpty(t, job.ID)
}

// TestFileCronJobStore_实现后端协议 编译期断言已在源码中。
func TestFileCronJobStore_实现后端协议(t *testing.T) {
	var backend CronJobStoreBackend = newTestStore(t)
	require.False(t, backend.SupportsWatch())
}
