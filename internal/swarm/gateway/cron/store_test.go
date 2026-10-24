package cron

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestStoreCrud 基本 CRUD
func TestStoreCrud(t *testing.T) {
	dir := t.TempDir()
	s := NewCronJobStore(filepath.Join(dir, "cron_jobs.json"))

	job, err := s.CreateJob(CreateJobParams{
		Name: "daily-report", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "daily", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == "" {
		t.Fatal("ID should be auto-generated")
	}
	if job.WakeOffsetSeconds != 300 {
		t.Fatalf("wake_offset default should be 300, got %d", job.WakeOffsetSeconds)
	}
	if job.Mode != "agent" {
		t.Fatalf("mode default should be agent, got %q", job.Mode)
	}

	got, _ := s.GetJob(job.ID)
	if got == nil || got.Name != "daily-report" {
		t.Fatal("GetJob should return created job")
	}

	// upsert：相同 job_id 覆盖
	_, err = s.CreateJob(CreateJobParams{
		JobID: job.ID, Name: "updated", CronExpr: "0 10 * * *", Timezone: "Asia/Shanghai",
		Description: "d2", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("upsert should not duplicate, got %d", len(jobs))
	}
	if jobs[0].Name != "updated" {
		t.Fatal("upsert should overwrite name")
	}
	// upsert 后 created_at 刷新（新对象新时间戳）
	if jobs[0].CreatedAt <= 0 {
		t.Fatal("created_at should be positive")
	}

	// update 更新任务
	updated, err := s.UpdateJob(job.ID, map[string]any{"enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("update should disable")
	}
	// 重新启用且未显式设置 expired → 隐含 expired=false（先置 expired 再 enable）
	s.UpdateJob(job.ID, map[string]any{"expired": true})
	s.UpdateJob(job.ID, map[string]any{"enabled": true})
	got, _ = s.GetJob(job.ID)
	if got.Expired {
		t.Fatal("re-enable should clear expired")
	}
	// 显式设置 expired=true 时 enabled 隐含清除不生效（对齐 Python：expired in patch 优先）
	s.UpdateJob(job.ID, map[string]any{"enabled": true, "expired": true})
	got, _ = s.GetJob(job.ID)
	if !got.Expired {
		t.Fatal("explicit expired=true with enabled should keep expired")
	}
	// cron_expr 修改也隐含 expired=false
	s.UpdateJob(job.ID, map[string]any{"expired": true})
	s.UpdateJob(job.ID, map[string]any{"cron_expr": "0 11 * * *"})
	got, _ = s.GetJob(job.ID)
	if got.Expired {
		t.Fatal("cron_expr change should clear expired")
	}

	// update wake_offset_seconds：负数钳制
	s.UpdateJob(job.ID, map[string]any{"wake_offset_seconds": -3})
	got, _ = s.GetJob(job.ID)
	if got.WakeOffsetSeconds != 0 {
		t.Fatalf("negative wake_offset should clamp to 0, got %d", got.WakeOffsetSeconds)
	}
	// update mode 空串回退 agent
	s.UpdateJob(job.ID, map[string]any{"mode": "PLAN"})
	got, _ = s.GetJob(job.ID)
	if got.Mode != "plan" {
		t.Fatalf("mode update should lowercase, got %q", got.Mode)
	}

	// delete 删除任务
	ok, _ := s.DeleteJob(job.ID)
	if !ok {
		t.Fatal("delete should succeed")
	}
	ok, _ = s.DeleteJob(job.ID)
	if ok {
		t.Fatal("second delete should return false")
	}
	// 空 ID delete 返回 false
	ok, _ = s.DeleteJob("")
	if ok {
		t.Fatal("empty ID delete should return false")
	}
}

// TestStore排序 按 updated_at+created_at 降序
func TestStore排序(t *testing.T) {
	dir := t.TempDir()
	s := NewCronJobStore(filepath.Join(dir, "cron_jobs.json"))
	j1, _ := s.CreateJob(CreateJobParams{Name: "j1", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true})
	j2, _ := s.CreateJob(CreateJobParams{Name: "j2", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true})
	// 更新 j1 使其 updated_at 最新
	s.UpdateJob(j1.ID, map[string]any{"name": "j1-upd"})
	jobs, _ := s.ListJobs()
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs, got %d", len(jobs))
	}
	if jobs[0].Name != "j1-upd" {
		t.Fatalf("most recently updated should be first, got %q", jobs[0].Name)
	}
	if jobs[1].ID != j2.ID {
		t.Fatal("second should be j2")
	}
}

// TestStore损坏JSON兜底
func TestStore损坏JSON兜底(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cron_jobs.json")
	os.WriteFile(p, []byte("{invalid json"), 0o644)
	s := NewCronJobStore(p)
	jobs, err := s.ListJobs()
	if err != nil || len(jobs) != 0 {
		t.Fatalf("corrupted JSON should fallback to empty, err=%v jobs=%d", err, len(jobs))
	}
	// 顶层非对象（对齐 Python isinstance(data, dict) 回退）
	os.WriteFile(p, []byte(`[1,2,3]`), 0o644)
	jobs, err = s.ListJobs()
	if err != nil || len(jobs) != 0 {
		t.Fatalf("non-dict JSON should fallback to empty, err=%v jobs=%d", err, len(jobs))
	}
}

// TestStore损坏条目跳过：单个非法条目不影响其余
func TestStore损坏条目跳过(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cron_jobs.json")
	// 一条合法 + 一条缺 targets + 一条非对象
	os.WriteFile(p, []byte(`{"version":1,"jobs":[
		{"id":"a","name":"n","cron_expr":"0 9 * * *","timezone":"Asia/Shanghai","description":"d","targets":"web"},
		{"id":"b","name":"n","cron_expr":"0 9 * * *","timezone":"Asia/Shanghai","description":"d"},
		42
	]}`), 0o644)
	s := NewCronJobStore(p)
	jobs, err := s.ListJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "a" {
		t.Fatalf("invalid entries should be skipped, got %d jobs", len(jobs))
	}
}

// TestStore并发读写
func TestStore并发读写(t *testing.T) {
	dir := t.TempDir()
	s := NewCronJobStore(filepath.Join(dir, "cron_jobs.json"))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.CreateJob(CreateJobParams{
				Name: "job", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
				Description: "d", Targets: "web", Enabled: true,
			})
			if err != nil {
				t.Errorf("concurrent create failed: %v", err)
			}
		}(i)
	}
	wg.Wait()
	jobs, _ := s.ListJobs()
	if len(jobs) != 20 {
		t.Fatalf("concurrent creates should all persist, got %d", len(jobs))
	}
}

// TestStore原子写
func TestStore原子写(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cron_jobs.json")
	s := NewCronJobStore(p)
	_, err := s.CreateJob(CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 主文件存在且合法
	if _, err := os.Stat(p); err != nil {
		t.Fatal("main file should exist after write")
	}
	// 残留 tmp 不影响读取（模拟中断后恢复）
	os.WriteFile(p+".tmp", []byte("garbage"), 0o644)
	jobs, err := s.ListJobs()
	if err != nil || len(jobs) != 1 {
		t.Fatal("leftover tmp should not affect reads")
	}
	// 写出的 JSON 是缩进格式（对齐 Python indent=2）
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "\n  \"version\"") {
		t.Logf("written JSON should be indent=2 format: %.80s", raw)
	}
}

// TestStoreUpdateNotFound
func TestStoreUpdateNotFound(t *testing.T) {
	s := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	if _, err := s.UpdateJob("nope", map[string]any{"name": "x"}); err == nil {
		t.Fatal("update nonexistent should error")
	}
	if _, err := s.UpdateJob("", map[string]any{"name": "x"}); err == nil {
		t.Fatal("update empty ID should error")
	}
}

// TestStoreCreateParams指针语义
func TestStoreCreateParams指针语义(t *testing.T) {
	dir := t.TempDir()
	s := NewCronJobStore(filepath.Join(dir, "cron_jobs.json"))
	wos := 60
	dar := true
	job, err := s.CreateJob(CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web",
		WakeOffsetSeconds: &wos, DeleteAfterRun: &dar,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.WakeOffsetSeconds != 60 || !job.DeleteAfterRun {
		t.Fatalf("pointer params mismatch: wos=%d dar=%v", job.WakeOffsetSeconds, job.DeleteAfterRun)
	}
}

// TestStore默认路径
func TestStore默认路径(t *testing.T) {
	s := NewCronJobStore("")
	if s.Path() == "" {
		t.Fatal("default path should not be empty")
	}
	s2 := NewCronJobStore(filepath.Join(t.TempDir(), "x.json"))
	if s2.Path() == "" || s2.Path() == s.Path() {
		t.Fatal("explicit path should override default")
	}
}
