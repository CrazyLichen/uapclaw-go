package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── health_check 别名 ────────────────────────────

// TestHealthCheck_注册为heartbeat别名 health_check.* 与 heartbeat.* 注册到同一对 handler。
func TestHealthCheck_注册为heartbeat别名(t *testing.T) {
	d := newHeartbeatTestChannel(&fakeHeartbeatSvc{})

	for _, method := range []string{
		"health_check.get_conf", "health_check.set_conf",
		"heartbeat.get_conf", "heartbeat.set_conf",
	} {
		_, ok := d.methodHandlers[method]
		assert.True(t, ok, "方法未注册: %s", method)
	}
}

// TestHealthCheck_缺服务文案 上游两个注册名共用 handler，文案统一为 health_check service not available。
func TestHealthCheck_缺服务文案(t *testing.T) {
	_, err := callHandler(heartbeatGetConf(newTestChannel(), nil), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrServiceUnavailable, errorCode(err))
	assert.Equal(t, "health_check service not available", err.Error())
}

// TestHealthCheckSetConf_飞书无目标 目标为 feishu 且通道没配推送目标时回 feishuNoTarget。
func TestHealthCheckSetConf_飞书无目标(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte("channels:\n  feishu:\n    apps:\n      - app_id: cli_a\n"), 0o644))

	svc := &fakeHeartbeatSvc{}
	_, err := callHandler(heartbeatSetConf(newTestChannel(), svc, nil), map[string]any{
		"target": "feishu",
	}, "")
	require.Error(t, err)
	assert.Equal(t, "feishuNoTarget", errorCode(err))
	assert.Equal(t, "feishuNoTarget", err.Error())
	assert.Nil(t, svc.lastTarget, "校验失败不得写入配置")
}

// TestHealthCheckSetConf_飞书有目标 每个 app 都有 last_chat_id 时通过校验并写入。
func TestHealthCheckSetConf_飞书有目标(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"channels:\n  feishu:\n    apps:\n      - app_id: cli_a\n        last_chat_id: oc_x\n"), 0o644))

	svc := &fakeHeartbeatSvc{}
	payload, err := callHandler(heartbeatSetConf(newTestChannel(), svc, nil), map[string]any{
		"target": "feishu",
	}, "")
	require.NoError(t, err)
	require.NotNil(t, svc.lastTarget)
	assert.Equal(t, "feishu", *svc.lastTarget, "校验通过后才写入")
	assert.NotNil(t, payload)
}

// TestHealthCheckSetConf_写回health_check段 写入键对齐 update_health_check_in_config。
func TestHealthCheckSetConf_写回health_check段(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)

	svc := &fakeHeartbeatSvc{}
	_, err := callHandler(heartbeatSetConf(newTestChannel(), svc, nil), map[string]any{"every": 120.0}, "")
	require.NoError(t, err)

	cfg, loadErr := loadAppConfig()
	require.NoError(t, loadErr)
	section := mapSection(cfg, "health_check")
	assert.Contains(t, section, "every", "写回键应为 health_check.every")
	assert.NotContains(t, mapSection(cfg, "heartbeat"), "every", "不得再写旧 heartbeat 段")
}

// ──────────────────────────── cron.job.meta ────────────────────────────

// TestCronJobMeta_返回schema 元数据来自 controller.JobMetadata()。
func TestCronJobMeta_返回schema(t *testing.T) {
	d := newCronTestDispatcher(&fakeCronJobController{})

	payload, err := dispatch(d, "cron.job.meta", nil, "s")
	require.NoError(t, err)
	assert.Equal(t, "agent", payload["default_mode"])
	assert.Equal(t, 3600, payload["default_timeout_seconds"])
	assert.Equal(t, 259200, payload["max_timeout_seconds"])
	assert.NotEmpty(t, payload["modes"])
}

// TestCronJobMeta_无控制器 缺控制器时回 cron not available。
func TestCronJobMeta_无控制器(t *testing.T) {
	_, err := callHandler(cronJobMeta(newTestChannel(), nil), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
	assert.Equal(t, "cron not available", err.Error())
}

// ──────────────────────────── updater（stub） ────────────────────────────

// TestUpdater_默认配置 http 层注册了 4 个方法，均回上游默认运行时配置。
func TestUpdater_默认配置(t *testing.T) {
	d := RegisterWebHandlers(nil)

	for _, method := range []string{"updater.get_conf", "updater.set_conf", "updater.reset_source"} {
		payload, err := dispatch(d, method, map[string]any{}, "")
		require.NoError(t, err, "%s 应成功返回默认配置", method)
		assert.Equal(t, "gitcode", payload["desktop_release_api_type"])
		assert.Equal(t, "openJiuwen", payload["repo_owner"])
		assert.Equal(t, 20, payload["timeout_seconds"])
	}

	payload, err := dispatch(d, "updater.upgrade", map[string]any{}, "")
	require.NoError(t, err)
	assert.Equal(t, "idle", payload["state"])
}

// TestHealthCheckSetConf_配置损坏 配置读不出来时飞书目标检测异常 → INTERNAL_ERROR。
func TestHealthCheckSetConf_配置损坏(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte("channels: [oops"), 0o644))

	svc := &fakeHeartbeatSvc{}
	_, err := callHandler(heartbeatSetConf(newTestChannel(), svc, nil), map[string]any{
		"target": "feishu",
	}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
}

// TestHealthCheckSetConf_写回失败仍回成功 写回 config.yaml 失败只 Warn，响应照旧成功。
func TestHealthCheckSetConf_写回失败仍回成功(t *testing.T) {
	useTempWorkspace(t)
	breakConfigFile(t)

	svc := &fakeHeartbeatSvc{}
	payload, err := callHandler(heartbeatSetConf(newTestChannel(), svc, nil), map[string]any{"every": 60.0}, "")
	require.NoError(t, err)
	assert.NotNil(t, payload)
}

// TestFeishuHasPushTarget_旧平铺 没有 apps 时看顶层 last_chat_id/chat_id。
func TestFeishuHasPushTarget_旧平铺(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))

	require.NoError(t, os.WriteFile(cfgPath, []byte("channels:\n  feishu:\n    last_chat_id: oc_1\n"), 0o644))
	ok, err := feishuHasPushTarget()
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, os.WriteFile(cfgPath, []byte("channels:\n  feishu:\n    app_id: cli_a\n"), 0o644))
	ok, err = feishuHasPushTarget()
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestFeishuHasPushTarget_非对象条目 apps 里有非对象条目时视为无目标。
func TestFeishuHasPushTarget_非对象条目(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte("channels:\n  feishu:\n    apps:\n      - junk\n"), 0o644))

	ok, err := feishuHasPushTarget()
	require.NoError(t, err)
	assert.False(t, ok)
}
