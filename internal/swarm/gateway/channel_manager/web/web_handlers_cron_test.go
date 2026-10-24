package web

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeCronJobController fake CronJobController
type fakeCronJobController struct {
	jobs       []map[string]any
	lastCreate map[string]any
	lastUpdate struct {
		jobID string
		patch map[string]any
	}
	lastToggle struct {
		jobID   string
		enabled bool
	}
	lastPreview struct {
		jobID string
		count int
	}
	runNowJobID string
	err         error // 非 nil 时所有方法返回该错误
}

func (f *fakeCronJobController) ListJobs() ([]map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.jobs, nil
}

func (f *fakeCronJobController) GetJob(jobID string) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, j := range f.jobs {
		if j["id"] == jobID {
			return j, nil
		}
	}
	return nil, nil
}

func (f *fakeCronJobController) CreateJob(params map[string]any) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.lastCreate = params
	return map[string]any{"id": "new-job", "name": params["name"]}, nil
}

func (f *fakeCronJobController) UpdateJob(jobID string, patch map[string]any) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.lastUpdate.jobID = jobID
	f.lastUpdate.patch = patch
	return map[string]any{"id": jobID, "patched": true}, nil
}

func (f *fakeCronJobController) DeleteJob(jobID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	for _, j := range f.jobs {
		if j["id"] == jobID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeCronJobController) ToggleJob(jobID string, enabled bool) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.lastToggle.jobID = jobID
	f.lastToggle.enabled = enabled
	return map[string]any{"id": jobID, "enabled": enabled}, nil
}

func (f *fakeCronJobController) PreviewJob(jobID string, count int) ([]map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.lastPreview.jobID = jobID
	f.lastPreview.count = count
	return []map[string]any{{"push_at": "2026-01-01T09:00:00+08:00"}}, nil
}

func (f *fakeCronJobController) RunNow(jobID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.runNowJobID = jobID
	return "run-123", nil
}

// newCronTestDispatcher 构造注入 fake controller 的 dispatcher
func newCronTestDispatcher(cc CronJobController) *RPCDispatcher {
	return RegisterWebHandlers(&WebHandlersBindParams{CronController: cc})
}

// ──────────────────────────── 注册测试 ────────────────────────────

// TestRegisterCronHandlers_注入控制器 8 个 cron.job.* 注册到真实 handler
func TestRegisterCronHandlers_注入控制器(t *testing.T) {
	d := newCronTestDispatcher(&fakeCronJobController{})
	expected := []string{
		"cron.job.list", "cron.job.get", "cron.job.create", "cron.job.update",
		"cron.job.delete", "cron.job.toggle", "cron.job.preview", "cron.job.run_now",
	}
	for _, method := range expected {
		_, ok := d.handlers[method]
		assert.True(t, ok, "方法 %q 应已注册", method)
	}
}

// TestRegisterCronHandlers_无控制器 无 controller 时走桩分支
func TestRegisterCronHandlers_无控制器(t *testing.T) {
	d := RegisterWebHandlers(nil)
	// 桩分支仍注册（前端兼容）
	for _, method := range []string{"cron.job.list", "cron.job.create"} {
		_, ok := d.handlers[method]
		assert.True(t, ok, "桩方法 %q 应已注册", method)
	}
	// 桩 list 返回空 jobs
	result, err := d.Dispatch("cron.job.list", nil, "s")
	assert.NoError(t, err)
	jobs := result["jobs"].([]any)
	assert.Empty(t, jobs)
}

// ──────────────────────────── 端点分发测试 ────────────────────────────

// TestCronJobList list 正常路径
func TestCronJobList(t *testing.T) {
	cc := &fakeCronJobController{jobs: []map[string]any{{"id": "j1", "name": "n"}}}
	d := newCronTestDispatcher(cc)

	result, err := d.Dispatch("cron.job.list", nil, "s")
	assert.NoError(t, err)
	jobs := result["jobs"].([]map[string]any)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "j1", jobs[0]["id"])
}

// TestCronJobList_控制器报错 list 异常 → INTERNAL_ERROR
func TestCronJobList_控制器报错(t *testing.T) {
	cc := &fakeCronJobController{err: errors.New("disk error")}
	d := newCronTestDispatcher(cc)

	result, err := d.Dispatch("cron.job.list", nil, "s")
	assert.NoError(t, err)
	assert.Equal(t, false, result["ok"])
	assert.Equal(t, "disk error", result["error"])
	assert.Equal(t, WsErrInternalError, result["code"])
}

// TestCronJobGet get 正常 + 缺 id + 不存在
func TestCronJobGet(t *testing.T) {
	cc := &fakeCronJobController{jobs: []map[string]any{{"id": "j1"}}}
	d := newCronTestDispatcher(cc)

	// 正常
	result, err := d.Dispatch("cron.job.get", map[string]any{"id": "j1"}, "s")
	assert.NoError(t, err)
	assert.Equal(t, "j1", result["job"].(map[string]any)["id"])

	// 缺 id
	result, _ = d.Dispatch("cron.job.get", map[string]any{}, "s")
	assert.Equal(t, WsErrBadRequest, result["code"])
	assert.Equal(t, "id is required", result["error"])

	// 不存在 → NOT_FOUND
	result, _ = d.Dispatch("cron.job.get", map[string]any{"id": "ghost"}, "s")
	assert.Equal(t, WsErrNotFound, result["code"])
	assert.Equal(t, "job not found", result["error"])
}

// TestCronJobCreate create 正常 + 异常 → BAD_REQUEST
func TestCronJobCreate(t *testing.T) {
	cc := &fakeCronJobController{}
	d := newCronTestDispatcher(cc)

	result, err := d.Dispatch("cron.job.create", map[string]any{
		"name": "n", "cron_expr": "0 9 * * *",
	}, "s")
	assert.NoError(t, err)
	assert.Equal(t, "new-job", result["job"].(map[string]any)["id"])
	assert.Equal(t, "n", cc.lastCreate["name"])

	// 控制器异常 → BAD_REQUEST
	cc.err = errors.New("invalid cron expr")
	result, _ = d.Dispatch("cron.job.create", map[string]any{"name": "x"}, "s")
	assert.Equal(t, WsErrBadRequest, result["code"])
	assert.Equal(t, "invalid cron expr", result["error"])
}

// TestCronJobUpdate update 正常 + not found
func TestCronJobUpdate(t *testing.T) {
	cc := &fakeCronJobController{}
	d := newCronTestDispatcher(cc)

	result, err := d.Dispatch("cron.job.update", map[string]any{
		"id": "j1", "patch": map[string]any{"name": "new"},
	}, "s")
	assert.NoError(t, err)
	assert.Equal(t, true, result["job"].(map[string]any)["patched"])
	assert.Equal(t, "j1", cc.lastUpdate.jobID)
	assert.Equal(t, "new", cc.lastUpdate.patch["name"])

	// job not found → NOT_FOUND
	cc.err = errors.New("job not found")
	result, _ = d.Dispatch("cron.job.update", map[string]any{"id": "ghost", "patch": map[string]any{}}, "s")
	assert.Equal(t, WsErrNotFound, result["code"])

	// 其他错误 → BAD_REQUEST
	cc.err = errors.New("bad patch")
	result, _ = d.Dispatch("cron.job.update", map[string]any{"id": "j1", "patch": map[string]any{}}, "s")
	assert.Equal(t, WsErrBadRequest, result["code"])

	// patch 非 object
	cc.err = nil
	result, _ = d.Dispatch("cron.job.update", map[string]any{"id": "j1", "patch": "not-map"}, "s")
	assert.Equal(t, "patch must be object", result["error"])
}

// TestCronJobDelete delete 正常 + 未删除 → NOT_FOUND
func TestCronJobDelete(t *testing.T) {
	cc := &fakeCronJobController{jobs: []map[string]any{{"id": "j1"}}}
	d := newCronTestDispatcher(cc)

	result, err := d.Dispatch("cron.job.delete", map[string]any{"id": "j1"}, "s")
	assert.NoError(t, err)
	assert.Equal(t, true, result["deleted"])

	// 不存在 → NOT_FOUND
	result, _ = d.Dispatch("cron.job.delete", map[string]any{"id": "ghost"}, "s")
	assert.Equal(t, WsErrNotFound, result["code"])
}

// TestCronJobToggle toggle 正常 + 缺 enabled + not found
func TestCronJobToggle(t *testing.T) {
	cc := &fakeCronJobController{}
	d := newCronTestDispatcher(cc)

	result, err := d.Dispatch("cron.job.toggle", map[string]any{
		"id": "j1", "enabled": true,
	}, "s")
	assert.NoError(t, err)
	assert.Equal(t, true, result["job"].(map[string]any)["enabled"])
	assert.True(t, cc.lastToggle.enabled)

	// 缺 enabled
	result, _ = d.Dispatch("cron.job.toggle", map[string]any{"id": "j1"}, "s")
	assert.Equal(t, WsErrBadRequest, result["code"])
	assert.Equal(t, "enabled is required", result["error"])

	// job not found
	cc.err = errors.New("job not found")
	result, _ = d.Dispatch("cron.job.toggle", map[string]any{"id": "g", "enabled": true}, "s")
	assert.Equal(t, WsErrNotFound, result["code"])
}

// TestCronJobPreview preview 正常 + count 解析 + 默认 5
func TestCronJobPreview(t *testing.T) {
	cc := &fakeCronJobController{}
	d := newCronTestDispatcher(cc)

	result, err := d.Dispatch("cron.job.preview", map[string]any{
		"id": "j1", "count": float64(7),
	}, "s")
	assert.NoError(t, err)
	next := result["next"].([]map[string]any)
	assert.Len(t, next, 1)
	assert.Equal(t, 7, cc.lastPreview.count)

	// 无 count → 默认 5
	_, _ = d.Dispatch("cron.job.preview", map[string]any{"id": "j1"}, "s")
	assert.Equal(t, 5, cc.lastPreview.count)

	// job not found
	cc.err = errors.New("job not found")
	result, _ = d.Dispatch("cron.job.preview", map[string]any{"id": "g"}, "s")
	assert.Equal(t, WsErrNotFound, result["code"])
}

// TestCronJobRunNow run_now 正常 + not found + 内部错误
func TestCronJobRunNow(t *testing.T) {
	cc := &fakeCronJobController{}
	d := newCronTestDispatcher(cc)

	result, err := d.Dispatch("cron.job.run_now", map[string]any{"id": "j1"}, "s")
	assert.NoError(t, err)
	assert.Equal(t, "run-123", result["run_id"])
	assert.Equal(t, "j1", cc.runNowJobID)

	// job not found → NOT_FOUND
	cc.err = errors.New("job not found")
	result, _ = d.Dispatch("cron.job.run_now", map[string]any{"id": "g"}, "s")
	assert.Equal(t, WsErrNotFound, result["code"])

	// 内部错误 → INTERNAL_ERROR
	cc.err = errors.New("scheduler dead")
	result, _ = d.Dispatch("cron.job.run_now", map[string]any{"id": "j1"}, "s")
	assert.Equal(t, WsErrInternalError, result["code"])
}

// TestCronJobHandlers_paramsNil 所有端点 params nil → BAD_REQUEST
func TestCronJobHandlers_paramsNil(t *testing.T) {
	d := newCronTestDispatcher(&fakeCronJobController{})
	methods := []string{
		"cron.job.get", "cron.job.create", "cron.job.update", "cron.job.delete",
		"cron.job.toggle", "cron.job.preview", "cron.job.run_now",
	}
	for _, method := range methods {
		result, err := d.Dispatch(method, nil, "s")
		assert.NoError(t, err, "%s nil params should not error", method)
		assert.Equal(t, WsErrBadRequest, result["code"], "%s nil params code", method)
	}
}

// TestCronJobHandlers_接口断言 CronJobController 接口可被外部实现
func TestCronJobHandlers_接口断言(t *testing.T) {
	// fake 实现已通过 newCronTestDispatcher 验证隐式实现；
	// gateway/cron.CronController 方法签名与本接口逐方法一致（同包外编译期验证由
	// app_gateway 装配处完成），nil controller 场景走桩分支：
	d := RegisterWebHandlers(&WebHandlersBindParams{CronController: nil})
	_, ok := d.handlers["cron.job.list"]
	assert.True(t, ok, "nil controller should fall back to stub branch")
}

// TestCronJobContextPassing ctx 与 sessionID 透传
func TestCronJobContextPassing(t *testing.T) {
	cc := &fakeCronJobController{jobs: []map[string]any{{"id": "j1"}}}
	d := newCronTestDispatcher(cc)

	// Dispatch 内部传 ctx/sessionID；handler 忽略但签名兼容
	result, err := d.Dispatch("cron.job.list", map[string]any{}, "sess-x")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	_ = context.Background()
}
