package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newChannelConfManager 构造带预置配置的 ChannelManager。
func newChannelConfManager(conf map[string]map[string]any) *cm.ChannelManager {
	return cm.NewChannelManager(nil, conf, nil)
}

// readChannelConfFromYAML 读回 config.yaml 里 channels.<platform> 的内容。
func readChannelConfFromYAML(t *testing.T, platform string) map[string]any {
	t.Helper()
	cfg, err := loadAppConfig()
	require.NoError(t, err)
	return mapSection(cfg, "channels", platform)
}

// ──────────────────────────── 注册与错误分支 ────────────────────────────

// TestChannelConf_九平台齐全 9 平台 × get/set = 18 个方法全部注册。
func TestChannelConf_九平台齐全(t *testing.T) {
	wc := newTestChannel()
	registerChannelConfFamily(wc, nil, nil)

	for _, platform := range channelConfPlatforms {
		for _, suffix := range []string{"get_conf", "set_conf"} {
			method := "channel." + platform + "." + suffix
			_, ok := wc.methodHandlers[method]
			assert.True(t, ok, "方法未注册: %s", method)
		}
	}
	assert.Len(t, channelConfPlatforms, 9, "上游共 9 个 IM 平台")
}

// TestChannelConf_无CM时回SERVICE_UNAVAILABLE 依赖缺失分支与上游文案一致。
func TestChannelConf_无CM时回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := newTestChannel()

	_, err := callHandler(channelConfGet(wc, nil, "feishu"), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrServiceUnavailable, errorCode(err))
	assert.Equal(t, "channel manager not available", err.Error())

	_, err = callHandler(channelConfSet(wc, nil, nil, "feishu"), map[string]any{}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrServiceUnavailable, errorCode(err))
}

// TestChannelConf_set_params非对象 BAD_REQUEST。
func TestChannelConf_set_params非对象(t *testing.T) {
	wc := newTestChannel()
	mgr := newChannelConfManager(nil)

	_, err := callHandler(channelConfSet(wc, mgr, nil, "telegram"), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
}

// TestChannelConf_feishu_set缺apps 上游 KeyError 被兜成 INTERNAL_ERROR。
func TestChannelConf_feishu_set缺apps(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)
	wc := newTestChannel()
	mgr := newChannelConfManager(nil)

	_, err := callHandler(channelConfSet(wc, mgr, nil, "feishu"), map[string]any{"other": 1}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
	assert.Equal(t, "'apps'", err.Error())
}

// ──────────────────────────── 普通平台往返 ────────────────────────────

// TestChannelConf_telegram往返 set 整体写入并回读，config.yaml 同步落盘。
func TestChannelConf_telegram往返(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)
	wc := newTestChannel()
	mgr := newChannelConfManager(nil)

	payload, err := callHandler(channelConfSet(wc, mgr, nil, "telegram"), map[string]any{
		"bot_token": "tkn",
		"enabled":   true,
	}, "")
	require.NoError(t, err)
	conf, _ := payload["config"].(map[string]any)
	assert.Equal(t, "tkn", conf["bot_token"])

	// 回读：get_conf 必须返回同一份
	got, getErr := callHandler(channelConfGet(wc, mgr, "telegram"), nil, "")
	require.NoError(t, getErr)
	gotConf, _ := got["config"].(map[string]any)
	assert.Equal(t, "tkn", gotConf["bot_token"])

	// 落盘：channels.telegram.bot_token 必须写进 config.yaml
	section := readChannelConfFromYAML(t, "telegram")
	assert.Equal(t, "tkn", section["bot_token"])
}

// ──────────────────────────── feishu 多应用 ────────────────────────────

// TestChannelConf_feishu_get_扁平转apps 旧平铺配置读出时转成 apps 且补缺省字段。
func TestChannelConf_feishu_get_扁平转apps(t *testing.T) {
	wc := newTestChannel()
	mgr := newChannelConfManager(map[string]map[string]any{
		"feishu": {"app_id": "cli_a", "app_secret": "sec"},
	})

	payload, err := callHandler(channelConfGet(wc, mgr, "feishu"), nil, "")
	require.NoError(t, err)
	conf, _ := payload["config"].(map[string]any)
	apps, _ := conf["apps"].([]any)
	require.Len(t, apps, 1)
	app, _ := apps[0].(map[string]any)
	assert.Equal(t, "cli_a", app["app_id"])
	assert.Equal(t, "sec", app["app_secret"])
	assert.Equal(t, true, app["is_default"], "旧平铺格式转为默认应用")
	assert.Equal(t, true, app["enable_streaming"], "缺省字段由 _FEISHU_APP_DEFAULTS 补齐")
	assert.Equal(t, []any{"0.0.0.0/0"}, app["allow_from"])
}

// TestChannelConf_feishu_set_按app_id合并保留运行时字段 同一 app_id 的既有字段被保留、
// 新值覆盖同名键。
// 注意（与上游同构）：归一化会先用 _FEISHU_APP_DEFAULTS 填满缺省键，而合并是
// `{**existing, **normalized}`，所以**缺省表里有的键**以归一化结果为准（含空串），
// 只有缺省表里没有的运行时字段（last_chat_id / bot_open_id 等）才真正被保留。
func TestChannelConf_feishu_set_按app_id合并保留运行时字段(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)
	wc := newTestChannel()
	mgr := newChannelConfManager(map[string]map[string]any{
		"feishu": {"apps": []any{map[string]any{"app_id": "cli_a", "last_chat_id": "oc_x"}}},
	})

	payload, err := callHandler(channelConfSet(wc, mgr, nil, "feishu"), map[string]any{
		"apps": []any{map[string]any{"app_id": "cli_a", "name": "新名字"}},
	}, "")
	require.NoError(t, err)
	conf, _ := payload["config"].(map[string]any)
	apps, _ := conf["apps"].([]any)
	require.Len(t, apps, 1)
	app, _ := apps[0].(map[string]any)
	assert.Equal(t, "oc_x", app["last_chat_id"], "缺省表之外的运行时字段必须保留")
	assert.Equal(t, "新名字", app["name"], "新值覆盖同名键")

	// 落盘：channels.feishu.apps 写入，且旧平铺键被清理
	section := readChannelConfFromYAML(t, "feishu")
	assert.Contains(t, section, "apps")
	assert.NotContains(t, section, "app_secret", "多应用模式下旧平铺键必须清理")
}

// TestChannelConf_xiaoyi_set_新app保留默认值 新增 app 时补 _XIAOYI_APP_DEFAULTS（含 ws 端点）。
func TestChannelConf_xiaoyi_set_新app保留默认值(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)
	wc := newTestChannel()
	mgr := newChannelConfManager(nil)

	payload, err := callHandler(channelConfSet(wc, mgr, nil, "xiaoyi"), map[string]any{
		"apps": []any{map[string]any{"app_id": "xy-1", "ak": "ak1"}},
	}, "")
	require.NoError(t, err)
	conf, _ := payload["config"].(map[string]any)
	apps, _ := conf["apps"].([]any)
	require.Len(t, apps, 1)
	app, _ := apps[0].(map[string]any)
	assert.Equal(t, "ak1", app["ak"])
	assert.Equal(t, "xiaoyi_channel", app["mode"])
	assert.Equal(t, "wss://hag.cloud.huawei.com/openclaw/v1/ws/link", app["ws_url1"])
}

// ──────────────────────────── 边界分支 ────────────────────────────

// TestChannelConf_apps非list apps 不是数组时回 INTERNAL_ERROR。
func TestChannelConf_apps非list(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)
	wc := newTestChannel()
	mgr := newChannelConfManager(nil)

	_, err := callHandler(channelConfSet(wc, mgr, nil, "feishu"), map[string]any{"apps": "oops"}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
	assert.Equal(t, "apps must be list", err.Error())
}

// TestChannelConf_feishu写回失败仍回成功 写回 config.yaml 失败只 Warn，响应照旧成功（对齐上游）。
func TestChannelConf_feishu写回失败仍回成功(t *testing.T) {
	useTempWorkspace(t)
	breakConfigFile(t)
	wc := newTestChannel()
	mgr := newChannelConfManager(nil)

	payload, err := callHandler(channelConfSet(wc, mgr, nil, "feishu"), map[string]any{
		"apps": []any{map[string]any{"app_id": "cli_a"}},
	}, "")
	require.NoError(t, err)
	conf, _ := payload["config"].(map[string]any)
	apps, _ := conf["apps"].([]any)
	assert.Len(t, apps, 1)
}

// TestChannelConf_普通平台写回失败仍回成功 同上，走 updateChannelInConfig 失败分支。
func TestChannelConf_普通平台写回失败仍回成功(t *testing.T) {
	useTempWorkspace(t)
	breakConfigFile(t)
	wc := newTestChannel()
	mgr := newChannelConfManager(nil)

	payload, err := callHandler(channelConfSet(wc, mgr, nil, "telegram"), map[string]any{
		"bot_token": "tkn",
	}, "")
	require.NoError(t, err)
	conf, _ := payload["config"].(map[string]any)
	assert.Equal(t, "tkn", conf["bot_token"])
}

// TestChannelConf_保留keepKeys 替换 apps 时 send_file_allowed 必须保留。
func TestChannelConf_保留keepKeys(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)
	// 预置 config.yaml：channels.feishu 同时有 send_file_allowed 与旧平铺键
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath,
		[]byte("channels:\n  feishu:\n    send_file_allowed: true\n    app_id: legacy\n"), 0o644))
	wc := newTestChannel()
	mgr := newChannelConfManager(nil)

	_, err := callHandler(channelConfSet(wc, mgr, nil, "feishu"), map[string]any{
		"apps": []any{map[string]any{"app_id": "cli_a"}},
	}, "")
	require.NoError(t, err)

	section := readChannelConfFromYAML(t, "feishu")
	assert.Equal(t, true, section["send_file_allowed"], "keepKeys 内的键必须保留")
	assert.NotContains(t, section, "app_id", "keepKeys 之外的旧平铺键必须清理")
}

// TestNormalizeChannelConf_nil 空配置的归一化：feishu/xiaoyi 回空 apps，其余回空 map。
func TestNormalizeChannelConf_nil(t *testing.T) {
	feishu := normalizeChannelConf("feishu", nil)
	assert.Equal(t, []any{}, feishu["apps"])

	xiaoyi := normalizeChannelConf("xiaoyi", nil)
	assert.Equal(t, []any{}, xiaoyi["apps"])

	telegram := normalizeChannelConf("telegram", nil)
	assert.Empty(t, telegram)
}

// TestChannelConf_xiaoyi_get_扁平转apps 旧平铺小艺配置读出时转成 apps 并补缺省字段。
func TestChannelConf_xiaoyi_get_扁平转apps(t *testing.T) {
	wc := newTestChannel()
	mgr := newChannelConfManager(map[string]map[string]any{
		"xiaoyi": {"ak": "ak1", "sk": "sk1"},
	})

	payload, err := callHandler(channelConfGet(wc, mgr, "xiaoyi"), nil, "")
	require.NoError(t, err)
	conf, _ := payload["config"].(map[string]any)
	apps, _ := conf["apps"].([]any)
	require.Len(t, apps, 1)
	app, _ := apps[0].(map[string]any)
	assert.Equal(t, "ak1", app["ak"])
	assert.Equal(t, true, app["is_default"])
	assert.Equal(t, "xiaoyi_channel", app["mode"])
}

// TestMergeAppsByID_边界 非对象条目、缺 app_id 的既有项、未命中的新项都按上游规则落位。
func TestMergeAppsByID_边界(t *testing.T) {
	// 既有清单里的非对象条目与缺 app_id 项被忽略
	existing := []any{"junk", map[string]any{"name": "无 id"}}
	newApps := []any{map[string]any{"app_id": "a"}, "junk-new", map[string]any{"app_id": "b"}}
	merged := mergeAppsByID(newApps, existing)
	assert.Equal(t, newApps, merged, "既有清单没有可用 app_id 时应原样返回新清单")

	// 命中 app_id 才合并，未命中与不可迭代项原样保留
	existing = []any{map[string]any{"app_id": "a", "keep": 1}}
	newApps = []any{map[string]any{"app_id": "a", "name": "n"}, map[string]any{"app_id": "b"}, "junk"}
	merged = mergeAppsByID(newApps, existing)
	require.Len(t, merged, 3)
	first, _ := merged[0].(map[string]any)
	assert.Equal(t, 1, first["keep"], "既有字段保留")
	assert.Equal(t, "n", first["name"], "新值覆盖")
	assert.Equal(t, "junk", merged[2], "非对象项原样保留")
}
