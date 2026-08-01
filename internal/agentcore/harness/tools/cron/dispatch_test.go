package cron

import (
	"context"
	"strings"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeCronBackend 用于测试的模拟 cron 后端
// 对齐 Python 各 backend 方法的调用参数和返回值
type fakeCronBackend struct {
	jobs           []map[string]any
	statusResult   map[string]any
	createdJobs    []map[string]any
	updatedJobs    map[string]map[string]any
	deletedJobIDs  []string
	toggledJobs    map[string]bool
	previewRecords map[string]int
	runNowIDs      []string
	runsResults    []map[string]any
	wakeRecords    map[string]string
	wakeResult     map[string]any
	getJobResult   map[string]any
	createJobCtx   *CronToolContext
	updateJobCtx   *CronToolContext
	wakeCtx        *CronToolContext
}

func newFakeCronBackend() *fakeCronBackend {
	return &fakeCronBackend{
		jobs: []map[string]any{
			{"id": "job1", "name": "测试任务"},
			{"id": "job2", "name": "第二个任务"},
		},
		statusResult:   map[string]any{"healthy": true},
		updatedJobs:    make(map[string]map[string]any),
		toggledJobs:    make(map[string]bool),
		previewRecords: make(map[string]int),
		wakeRecords:    make(map[string]string),
		wakeResult:     map[string]any{"wake_id": "wake_1"},
		getJobResult:   map[string]any{"id": "job1", "name": "测试任务"},
		createdJobs:    []map[string]any{},
		deletedJobIDs:  []string{},
		runNowIDs:      []string{},
	}
}

func (f *fakeCronBackend) ListJobs(ctx context.Context, includeDisabled bool) ([]map[string]any, error) {
	return f.jobs, nil
}

func (f *fakeCronBackend) GetJob(ctx context.Context, jobID string) (map[string]any, error) {
	return f.getJobResult, nil
}

func (f *fakeCronBackend) CreateJob(ctx context.Context, params map[string]any, cronCtx *CronToolContext) (map[string]any, error) {
	f.createdJobs = append(f.createdJobs, params)
	f.createJobCtx = cronCtx
	return map[string]any{"id": "new_job", "params": params}, nil
}

func (f *fakeCronBackend) UpdateJob(ctx context.Context, jobID string, patch map[string]any, cronCtx *CronToolContext) (map[string]any, error) {
	f.updatedJobs[jobID] = patch
	f.updateJobCtx = cronCtx
	return map[string]any{"id": jobID, "patched": true}, nil
}

func (f *fakeCronBackend) DeleteJob(ctx context.Context, jobID string) (bool, error) {
	f.deletedJobIDs = append(f.deletedJobIDs, jobID)
	return true, nil
}

func (f *fakeCronBackend) ToggleJob(ctx context.Context, jobID string, enabled bool) (map[string]any, error) {
	f.toggledJobs[jobID] = enabled
	return map[string]any{"id": jobID, "enabled": enabled}, nil
}

func (f *fakeCronBackend) PreviewJob(ctx context.Context, jobID string, count int) ([]map[string]any, error) {
	f.previewRecords[jobID] = count
	return []map[string]any{{"time": "2026-01-01T09:00:00"}}, nil
}

func (f *fakeCronBackend) RunNow(ctx context.Context, jobID string) (string, error) {
	f.runNowIDs = append(f.runNowIDs, jobID)
	return "run_123", nil
}

func (f *fakeCronBackend) Status(ctx context.Context) (map[string]any, error) {
	return f.statusResult, nil
}

func (f *fakeCronBackend) GetRuns(ctx context.Context, jobID string, limit int) ([]map[string]any, error) {
	return f.runsResults, nil
}

func (f *fakeCronBackend) Wake(ctx context.Context, text string, cronCtx *CronToolContext, mode string) (map[string]any, error) {
	f.wakeRecords[text] = mode
	f.wakeCtx = cronCtx
	return f.wakeResult, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDispatchCronAction_status 测试 action=status
// 对齐 Python L172: return await backend.status()
func TestDispatchCronAction_status(t *testing.T) {
	backend := newFakeCronBackend()
	result, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "status"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["healthy"] != true {
		t.Errorf("result = %v, want healthy=true", result)
	}
}

// TestDispatchCronAction_list 测试 action=list
// 对齐 Python L174: return {"jobs": await backend.list_jobs(...)}
func TestDispatchCronAction_list(t *testing.T) {
	backend := newFakeCronBackend()
	result, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "list", "includeDisabled": true}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jobs, ok := result["jobs"].([]map[string]any)
	if !ok || len(jobs) != 2 {
		t.Errorf("result['jobs'] = %v, want 2 jobs", result["jobs"])
	}
}

// TestDispatchCronAction_add_用job对象 测试 action=add，使用 job 对象
// 对齐 Python L176-179: create_input = dict(job or {})
func TestDispatchCronAction_add_用job对象(t *testing.T) {
	backend := newFakeCronBackend()
	cronCtx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_1"}
	jobObj := map[string]any{"name": "每天9点", "cron_expr": "0 0 9 * * ? *"}
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "add", "job": jobObj}, cronCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(backend.createdJobs) != 1 {
		t.Fatalf("createdJobs count = %d, want 1", len(backend.createdJobs))
	}
	if backend.createdJobs[0]["name"] != "每天9点" {
		t.Errorf("createdJob name = %v, want '每天9点'", backend.createdJobs[0]["name"])
	}
	if backend.createJobCtx != cronCtx {
		t.Errorf("createJobCtx not equal to expected cronCtx")
	}
}

// TestDispatchCronAction_add_用flatKwargs 测试 action=add，job 为空，使用 flat_kwargs
// 对齐 Python L178: if not create_input: create_input = flat_kwargs
func TestDispatchCronAction_add_用flatKwargs(t *testing.T) {
	backend := newFakeCronBackend()
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{
		"action": "add", "name": "每小时", "cron_expr": "0 0 * * * ? *",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(backend.createdJobs) != 1 {
		t.Fatalf("createdJobs count = %d, want 1", len(backend.createdJobs))
	}
	if backend.createdJobs[0]["name"] != "每小时" {
		t.Errorf("createdJob name = %v, want '每小时'", backend.createdJobs[0]["name"])
	}
}

// TestDispatchCronAction_update 测试 action=update
// 对齐 Python L180-186
func TestDispatchCronAction_update(t *testing.T) {
	backend := newFakeCronBackend()
	cronCtx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_1"}
	patch := map[string]any{"enabled": false}
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{
		"action": "update", "jobId": "job1", "patch": patch,
	}, cronCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.updatedJobs["job1"]["enabled"] != false {
		t.Errorf("updatedJob patch not recorded correctly")
	}
	if backend.updateJobCtx != cronCtx {
		t.Errorf("updateJobCtx not equal to expected cronCtx")
	}
}

// TestDispatchCronAction_update_缺jobId 测试 action=update，jobId 缺失
// 对齐 Python L182: raise ValueError("jobId is required")
func TestDispatchCronAction_update_缺jobId(t *testing.T) {
	backend := newFakeCronBackend()
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "update"}, nil)
	if err == nil {
		t.Fatalf("expected error for missing jobId")
	}
	if err.Error() != "jobId is required" {
		t.Errorf("error = %q, want 'jobId is required'", err.Error())
	}
}

// TestDispatchCronAction_remove 测试 action=remove
// 对齐 Python L187-190: return {"deleted": await backend.delete_job(target_job_id)}
func TestDispatchCronAction_remove(t *testing.T) {
	backend := newFakeCronBackend()
	result, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "remove", "jobId": "job1"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted, ok := result["deleted"].(bool); !ok || !deleted {
		t.Errorf("result['deleted'] = %v, want true", result["deleted"])
	}
}

// TestDispatchCronAction_run 测试 action=run
// 对齐 Python L191-194: return {"run_id": await backend.run_now(target_job_id)}
func TestDispatchCronAction_run(t *testing.T) {
	backend := newFakeCronBackend()
	result, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "run", "jobId": "job1"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runID, ok := result["run_id"].(string); !ok || runID != "run_123" {
		t.Errorf("result['run_id'] = %v, want 'run_123'", result["run_id"])
	}
}

// TestDispatchCronAction_runs 测试 action=runs
// 对齐 Python L195-198: return {"runs": await backend.get_runs(target_job_id)}
func TestDispatchCronAction_runs(t *testing.T) {
	backend := newFakeCronBackend()
	backend.runsResults = []map[string]any{{"run_id": "r1", "status": "completed"}}
	result, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "runs", "jobId": "job1"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["runs"]; !ok {
		t.Errorf("result should contain 'runs' key")
	}
}

// TestDispatchCronAction_wake 测试 action=wake
// 对齐 Python L199-200: return await backend.wake(text or "", context=context, mode=mode)
func TestDispatchCronAction_wake(t *testing.T) {
	backend := newFakeCronBackend()
	cronCtx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_1"}
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{
		"action": "wake", "text": "提醒开会", "mode": "now",
	}, cronCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.wakeRecords["提醒开会"] != "now" {
		t.Errorf("wakeRecord = %v, want mode='now'", backend.wakeRecords)
	}
	if backend.wakeCtx != cronCtx {
		t.Errorf("wakeCtx not equal to expected cronCtx")
	}
}

// TestDispatchCronAction_不支持的action 测试未知 action
// 对齐 Python L201: raise ValueError("unsupported cron action")
func TestDispatchCronAction_不支持的action(t *testing.T) {
	backend := newFakeCronBackend()
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "invalid"}, nil)
	if err == nil {
		t.Fatalf("expected error for unsupported action")
	}
	if !strings.Contains(err.Error(), "unsupported cron action") {
		t.Errorf("error = %q, should contain 'unsupported cron action'", err.Error())
	}
}

// TestDispatchCronAction_excludedKeys过滤 测试 excluded_keys 过滤
// 对齐 Python: flat_kwargs 排除 excluded_keys (cron.py L152-170)
func TestDispatchCronAction_excludedKeys过滤(t *testing.T) {
	backend := newFakeCronBackend()
	inputs := map[string]any{
		"action": "add", "name": "测试",
		"gatewayUrl": "http://gateway", "gatewayToken": "secret",
		"timeoutMs": 5000, "runMode": "sync",
	}
	_, err := dispatchCronAction(context.Background(), backend, inputs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(backend.createdJobs) != 1 {
		t.Fatalf("createdJobs count = %d, want 1", len(backend.createdJobs))
	}
	job := backend.createdJobs[0]
	if _, ok := job["gatewayUrl"]; ok {
		t.Errorf("createdJob should not contain 'gatewayUrl'")
	}
	if _, ok := job["gatewayToken"]; ok {
		t.Errorf("createdJob should not contain 'gatewayToken'")
	}
	if _, ok := job["timeoutMs"]; ok {
		t.Errorf("createdJob should not contain 'timeoutMs'")
	}
	if _, ok := job["runMode"]; ok {
		t.Errorf("createdJob should not contain 'runMode'")
	}
	if job["name"] != "测试" {
		t.Errorf("createdJob name = %v, want '测试'", job["name"])
	}
}

// TestDispatchCronAction_legacyId兼容 测试 kwargs 中的 "id" 作为 jobId 兼容
// 对齐 Python L150: legacy_job_id = kwargs.pop("id", None)
func TestDispatchCronAction_legacyId兼容(t *testing.T) {
	backend := newFakeCronBackend()
	result, err := dispatchCronAction(context.Background(), backend, map[string]any{
		"action": "remove", "id": "legacy_job_1",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted, ok := result["deleted"].(bool); !ok || !deleted {
		t.Errorf("result['deleted'] = %v, want true", result["deleted"])
	}
	if len(backend.deletedJobIDs) != 1 || backend.deletedJobIDs[0] != "legacy_job_1" {
		t.Errorf("deletedJobIDs = %v, want ['legacy_job_1']", backend.deletedJobIDs)
	}
}

// TestDispatchCronAction_remove_缺jobId 测试 action=remove，jobId 缺失
func TestDispatchCronAction_remove_缺jobId(t *testing.T) {
	backend := newFakeCronBackend()
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "remove"}, nil)
	if err == nil {
		t.Fatalf("expected error for missing jobId")
	}
	if err.Error() != "jobId is required" {
		t.Errorf("error = %q, want 'jobId is required'", err.Error())
	}
}

// TestDispatchCronAction_run_缺jobId 测试 action=run，jobId 缺失
func TestDispatchCronAction_run_缺jobId(t *testing.T) {
	backend := newFakeCronBackend()
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "run"}, nil)
	if err == nil {
		t.Fatalf("expected error for missing jobId")
	}
	if err.Error() != "jobId is required" {
		t.Errorf("error = %q, want 'jobId is required'", err.Error())
	}
}

// TestDispatchCronAction_runs_缺jobId 测试 action=runs，jobId 缺失
func TestDispatchCronAction_runs_缺jobId(t *testing.T) {
	backend := newFakeCronBackend()
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "runs"}, nil)
	if err == nil {
		t.Fatalf("expected error for missing jobId")
	}
	if err.Error() != "jobId is required" {
		t.Errorf("error = %q, want 'jobId is required'", err.Error())
	}
}

// TestDispatchCronAction_wake_空text 测试 action=wake，text 为空
// 对齐 Python L200: await backend.wake(text or "", ...)
func TestDispatchCronAction_wake_空text(t *testing.T) {
	backend := newFakeCronBackend()
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{"action": "wake"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := backend.wakeRecords[""]; !ok {
		t.Errorf("wakeRecords should contain empty text key")
	}
}

// TestDispatchCronAction_update_用flatKwargs 测试 action=update，patch 为空，使用 flat_kwargs
// 对齐 Python L184-185: if not patch_input: patch_input = flat_kwargs
func TestDispatchCronAction_update_用flatKwargs(t *testing.T) {
	backend := newFakeCronBackend()
	_, err := dispatchCronAction(context.Background(), backend, map[string]any{
		"action": "update", "jobId": "job1", "enabled": false,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.updatedJobs["job1"]["enabled"] != false {
		t.Errorf("updatedJob patch = %v, want enabled=false", backend.updatedJobs["job1"])
	}
}

// TestStrVal 测试 strVal 辅助函数
func TestStrVal(t *testing.T) {
	m := map[string]any{
		"key1": "hello",
		"key2": 42,
		"key3": true,
	}
	if strVal(m, "key1") != "hello" {
		t.Errorf("strVal(key1) = %q, want 'hello'", strVal(m, "key1"))
	}
	if strVal(m, "key2") != "42" {
		t.Errorf("strVal(key2) = %q, want '42'", strVal(m, "key2"))
	}
	if strVal(m, "missing") != "" {
		t.Errorf("strVal(missing) = %q, want ''", strVal(m, "missing"))
	}
}

// TestBoolVal 测试 boolVal 辅助函数（含非 bool 类型路径）
func TestBoolVal(t *testing.T) {
	m := map[string]any{
		"key1": true,
		"key2": false,
		"key3": "not_bool",
		"key4": 42,
	}
	if boolVal(m, "key1") != true {
		t.Errorf("boolVal(key1) = %v, want true", boolVal(m, "key1"))
	}
	if boolVal(m, "key2") != false {
		t.Errorf("boolVal(key2) = %v, want false", boolVal(m, "key2"))
	}
	// 非 bool 类型应返回 false
	if boolVal(m, "key3") != false {
		t.Errorf("boolVal(key3) = %v, want false (string not bool)", boolVal(m, "key3"))
	}
	if boolVal(m, "key4") != false {
		t.Errorf("boolVal(key4) = %v, want false (int not bool)", boolVal(m, "key4"))
	}
	if boolVal(m, "missing") != false {
		t.Errorf("boolVal(missing) = %v, want false", boolVal(m, "missing"))
	}
}

// TestMapVal 测试 mapVal 辅助函数（含非 map 类型路径）
func TestMapVal(t *testing.T) {
	m := map[string]any{
		"key1": map[string]any{"nested": "value"},
		"key2": "not_map",
	}
	result := mapVal(m, "key1")
	if result["nested"] != "value" {
		t.Errorf("mapVal(key1) = %v, want nested=value", result)
	}
	result = mapVal(m, "key2")
	if len(result) != 0 {
		t.Errorf("mapVal(key2) = %v, want empty map (string not map)", result)
	}
	result = mapVal(m, "missing")
	if len(result) != 0 {
		t.Errorf("mapVal(missing) = %v, want empty map", result)
	}
}
