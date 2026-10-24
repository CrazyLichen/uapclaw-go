package cron

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestControllerNormalizeTargets targets 归一化矩阵
func TestControllerNormalizeTargets(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	// 空且无默认 channel → 报错
	if _, err := c.normalizeTargets(""); err == nil {
		t.Error("empty targets without default channel should error")
	}
	// 设置默认 channel 后空 targets → 默认值
	c.SetTargetChannel(TargetChannelTUI)
	if got, err := c.normalizeTargets(""); err != nil || got != "tui" {
		t.Errorf("empty targets with default = %q, err=%v", got, err)
	}
	// 大小写归一
	if got, err := c.normalizeTargets("WEB"); err != nil || got != "web" {
		t.Errorf("case normalize = %q, err=%v", got, err)
	}
	// 非法值报错
	if _, err := c.normalizeTargets("nope"); err == nil {
		t.Error("invalid targets should error")
	}
	// feishu_enterprise 前缀形式合法
	if got, err := c.normalizeTargets("feishu_enterprise:cli_x"); err != nil || got != "feishu_enterprise:cli_x" {
		t.Errorf("enterprise prefix = %q, err=%v", got, err)
	}
}

// TestControllerNormalizeDescription description 时间关键词兜底
func TestControllerNormalizeDescription(t *testing.T) {
	// 含时间关键词且 name 是子串 → 用 name
	if got := normalizeDescription("每天早上九点汇报", "汇报"); got != "汇报" {
		t.Errorf("time keyword + name substring should use name, got %q", got)
	}
	// 无时间关键词 → 保留 description
	if got := normalizeDescription("写周报", "周报"); got != "写周报" {
		t.Errorf("no time keyword should keep description, got %q", got)
	}
	// name 为空 → 保留 description
	if got := normalizeDescription("每天早上九点汇报", ""); got != "每天早上九点汇报" {
		t.Errorf("empty name should keep description, got %q", got)
	}
	// 含时间关键词但 name 不是子串 → 保留 description
	if got := normalizeDescription("每天早上九点做功课", "学习"); got != "每天早上九点做功课" {
		t.Errorf("name not substring should keep description, got %q", got)
	}
	// 前缀包含也算（Contains 语义）
	if got := normalizeDescription("汇报每天情况", "汇报"); got != "汇报" {
		t.Errorf("name prefix should use name, got %q", got)
	}
}

// TestControllerRoutingSessionID routing session_id 推导矩阵
func TestControllerRoutingSessionID(t *testing.T) {
	// feishu_enterprise 非 SessionMap → 空
	if got := routingSessionID("feishu_enterprise:app1", "plain-sid"); got != "" {
		t.Errorf("enterprise non-SessionMap should be empty, got %q", got)
	}
	// feishu_enterprise 合法 SessionMap → 原样
	want := "feishu::oc_1::bot::ou_2"
	if got := routingSessionID("feishu_enterprise:app1", want); got != want {
		t.Errorf("enterprise SessionMap should pass, got %q", got)
	}
	// 普通渠道 → 原样
	if got := routingSessionID("web", "plain-sid"); got != "plain-sid" {
		t.Errorf("web should accept plain sid, got %q", got)
	}
	// 空入参 → 空
	if got := routingSessionID("web", ""); got != "" {
		t.Errorf("empty raw should be empty, got %q", got)
	}
	// 非字符串 → 空
	if got := routingSessionID("web", 123); got != "" {
		t.Errorf("non-string raw should be empty, got %q", got)
	}
	// SessionMap 但首段非 feishu → 空
	if got := routingSessionID("feishu_enterprise:app1", "wecom::oc::bot"); got != "" {
		t.Errorf("non-feishu SessionMap should be empty, got %q", got)
	}
	// SessionMap 但段数不足 → 空
	if got := routingSessionID("feishu_enterprise:app1", "feishu::oc"); got != "" {
		t.Errorf("short SessionMap should be empty, got %q", got)
	}
}

// TestControllerCreateJob全链路 创建任务归一化+reload 联动
func TestControllerCreateJob全链路(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	d, err := c.CreateJob(map[string]any{
		"name": "汇报", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai",
		"description": "每天早上汇报", "targets": "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	// description 含时间关键词且 name 是子串 → 折叠为 name
	if d["description"] != "汇报" {
		t.Errorf("description should collapse to name, got %v", d["description"])
	}
	// cron_expr 归一化为 7 域（前缀 "0 " 后缀 " *"）
	if d["cron_expr"] != "0 0 9 * * * *" {
		t.Errorf("cron_expr should be normalized, got %v", d["cron_expr"])
	}
	// enabled 缺省 true
	if d["enabled"] != true {
		t.Errorf("enabled default = %v, want true", d["enabled"])
	}
	// scheduler jobs 已加载
	svc.mu.Lock()
	n := len(svc.jobs)
	svc.mu.Unlock()
	if n != 1 {
		t.Errorf("scheduler should reload 1 job, got %d", n)
	}
}

// TestControllerCreateJob错误路径 校验失败传播
func TestControllerCreateJob错误路径(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	// 非法表达式
	if _, err := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "bad expr", "targets": "web",
	}); err == nil {
		t.Error("invalid cron_expr should error")
	}
	// targets 缺失且无默认 channel
	if _, err := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "targets": "",
	}); err == nil {
		t.Error("missing targets should error")
	}
	// 非法时区
	if _, err := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "Not/AZone", "targets": "web",
	}); err == nil {
		t.Error("invalid timezone should error")
	}
	// wake_offset_seconds 非整数
	if _, err := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "targets": "web",
		"wake_offset_seconds": "abc",
	}); err == nil {
		t.Error("non-int wake_offset_seconds should error")
	}
}

// TestControllerUpdateJob 更新联动（校验 + session_id 重推导）
func TestControllerUpdateJob(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	d, err := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web", "session_id": "sid-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := d["id"].(string)

	// 不存在的任务
	if _, err := c.UpdateJob("ghost", map[string]any{"name": "x"}); err == nil {
		t.Error("nonexistent job should error")
	}

	// cron_expr 更新触发归一化
	d2, err := c.UpdateJob(jobID, map[string]any{"cron_expr": "30 8 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	if d2["cron_expr"] != "0 30 8 * * * *" {
		t.Errorf("updated cron_expr = %v", d2["cron_expr"])
	}

	// targets 更新联动 session_id 重推导（web 渠道 sid 原样保留）
	d3, err := c.UpdateJob(jobID, map[string]any{"targets": "web"})
	if err != nil {
		t.Fatal(err)
	}
	if d3["session_id"] != "sid-1" {
		t.Errorf("targets update should re-derive session_id, got %v", d3["session_id"])
	}

	// description 更新走 normalizeDescription（name="n" 不是子串 → 保留原文）
	d4, err := c.UpdateJob(jobID, map[string]any{"description": "每周一汇报工作"})
	if err != nil {
		t.Fatal(err)
	}
	if d4["description"] != "每周一汇报工作" {
		t.Errorf("description without name substring should keep original, got %v", d4["description"])
	}
}

// TestControllerUpdateJob校验失败 无效表达式更新被拒
func TestControllerUpdateJob校验失败(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	d, err := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := d["id"].(string)

	// 同时给 cron_expr+timezone：timezone 非法 → 校验失败
	if _, err := c.UpdateJob(jobID, map[string]any{"cron_expr": "0 8 * * *", "timezone": "Bad/Zone"}); err == nil {
		t.Error("invalid timezone in patch should fail validation")
	}
}

// TestControllerDeleteJob 删除联动 reload
func TestControllerDeleteJob(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	d, _ := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web",
	})
	jobID, _ := d["id"].(string)

	deleted, err := c.DeleteJob(jobID)
	if err != nil || !deleted {
		t.Fatalf("delete = %v, err=%v", deleted, err)
	}
	// 不存在的删除返回 false
	deleted2, err := c.DeleteJob(jobID)
	if err != nil || deleted2 {
		t.Errorf("second delete = %v, err=%v", deleted2, err)
	}
	// jobs map 已清空
	svc.mu.Lock()
	_, has := svc.jobs[jobID]
	svc.mu.Unlock()
	if has {
		t.Error("deleted job should be removed from scheduler jobs")
	}
}

// TestControllerToggleJob 启停联动
func TestControllerToggleJob(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	d, _ := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web",
	})
	jobID, _ := d["id"].(string)

	d2, err := c.ToggleJob(jobID, false)
	if err != nil {
		t.Fatal(err)
	}
	if d2["enabled"] != false {
		t.Errorf("toggled enabled = %v", d2["enabled"])
	}
	// 停用后 scheduler 无事件
	svc.mu.Lock()
	nEvents := len(svc.events)
	svc.mu.Unlock()
	if nEvents != 0 {
		t.Errorf("disabled job should have no events, got %d", nEvents)
	}
}

// TestControllerPreviewJob 预览下 N 次
func TestControllerPreviewJob(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	job, err := store.CreateJob(CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	rows, err := c.PreviewJob(job.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Errorf("preview should return 3 rows, got %d", len(rows))
	}
	// 递增时间
	for i := 1; i < len(rows); i++ {
		if strAny(rows[i]["push_at"]) <= strAny(rows[i-1]["push_at"]) {
			t.Errorf("push_at should be increasing: %v", rows)
		}
	}
	// wake_at = push_at - 300s（默认 wake_offset）
	if rows[0]["wake_at"] == rows[0]["push_at"] {
		t.Error("wake_at should be before push_at")
	}
	// count=0 钳制到 1
	rows, _ = c.PreviewJob(job.ID, 0)
	if len(rows) != 1 {
		t.Errorf("count=0 should clamp to 1, got %d", len(rows))
	}
	// count=100 钳制到 50
	rows, _ = c.PreviewJob(job.ID, 100)
	if len(rows) != 50 {
		t.Errorf("count=100 should clamp to 50, got %d", len(rows))
	}
	// 不存在
	if _, err := c.PreviewJob("nope", 5); err == nil {
		t.Error("nonexistent job should error")
	}
}

// TestControllerPreviewJob过期 过期 one-shot 预览为空
func TestControllerPreviewJob过期(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	job, err := store.CreateJob(CreateJobParams{
		Name: "old", CronExpr: "0 0 17 28 12 * 2020", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	rows, err := c.PreviewJob(job.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expired one-shot should return 0 rows, got %d", len(rows))
	}
}

// TestControllerListAndGet list/get 委托
func TestControllerListAndGet(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	d, _ := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web",
	})
	jobID, _ := d["id"].(string)

	list, err := c.ListJobs()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, err=%v", len(list), err)
	}
	got, err := c.GetJob(jobID)
	if err != nil || got == nil {
		t.Fatalf("get = %v, err=%v", got, err)
	}
	if got["id"] != jobID {
		t.Errorf("get id = %v", got["id"])
	}
	// 不存在返回 nil,nil
	missing, err := c.GetJob("ghost")
	if err != nil || missing != nil {
		t.Errorf("missing = %v, err=%v", missing, err)
	}
}

// TestControllerRunNow 立即执行委托
func TestControllerRunNow(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	job, err := store.CreateJob(CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	runID, err := c.RunNow(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(runID, job.ID+":") {
		t.Errorf("runID = %q", runID)
	}
	// 事件已入队（wake+push 各一）
	svc.mu.Lock()
	nEvents := len(svc.events)
	svc.mu.Unlock()
	if nEvents != 2 {
		t.Errorf("run_now should enqueue 2 events, got %d", nEvents)
	}
}
