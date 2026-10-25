package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeHeartbeatSvc HeartbeatConfService 的测试桩。
// SetHeartbeatConf 应用更新到内部 conf（使写回断言与 set 值一致）。
type fakeHeartbeatSvc struct {
	conf       map[string]any
	setErr     error
	lastEvery  *float64
	lastTarget *string
	lastAH     map[string]any
}

func (f *fakeHeartbeatSvc) GetHeartbeatConf() map[string]any {
	if f.conf == nil {
		return map[string]any{"every": 60.0, "target": nil, "active_hours": nil}
	}
	return f.conf
}

func (f *fakeHeartbeatSvc) SetHeartbeatConf(every *float64, target *string, activeHours map[string]any) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.lastEvery = every
	f.lastTarget = target
	f.lastAH = activeHours
	if f.conf == nil {
		f.conf = map[string]any{"every": 60.0, "target": nil, "active_hours": nil}
	}
	if every != nil {
		f.conf["every"] = *every
	}
	if target != nil {
		f.conf["target"] = *target
	}
	if activeHours != nil {
		f.conf["active_hours"] = activeHours
	}
	return nil
}

// newHeartbeatTestDispatcher 构造注入 fake 服务的 dispatcher。
func newHeartbeatTestDispatcher(hb HeartbeatConfService) *RPCDispatcher {
	return RegisterWebHandlers(&WebHandlersBindParams{HeartbeatService: hb})
}

// newHeartbeatTestDispatcherWithClient 带 agentClient 的 dispatcher（nil 即纯服务注入）。
func newHeartbeatTestDispatcherWithClient(hb HeartbeatConfService, ac *routing.AgentClient) *RPCDispatcher {
	return RegisterWebHandlers(&WebHandlersBindParams{HeartbeatService: hb, AgentClient: ac})
}

// ──────────────────────────── 注册测试 ────────────────────────────

// TestRegisterHeartbeatHandlers_注入服务 三个方法注册（Dispatch 探测而非直接访问 handlers map）。
func TestRegisterHeartbeatHandlers_注入服务(t *testing.T) {
	d := newHeartbeatTestDispatcher(&fakeHeartbeatSvc{})
	// Dispatch 三参数：method, params, sessionID（无 ctx——handler 内部自建 context.Background()）
	_, err := d.Dispatch("heartbeat.get_conf", map[string]any{}, "")
	require.NoError(t, err, "get_conf should dispatch")
	_, err = d.Dispatch("heartbeat.set_conf", map[string]any{}, "")
	require.NoError(t, err, "set_conf should dispatch")
	_, err = d.Dispatch("heartbeat.get_path", map[string]any{}, "")
	require.NoError(t, err, "get_path should dispatch")
}

// TestRegisterHeartbeatHandlers_无服务 保留三个 stub。
func TestRegisterHeartbeatHandlers_无服务(t *testing.T) {
	d := RegisterWebHandlers(&WebHandlersBindParams{})
	for _, m := range []string{"heartbeat.get_conf", "heartbeat.set_conf", "heartbeat.get_path"} {
		res, err := d.Dispatch(m, map[string]any{}, "")
		require.NoError(t, err, "stub %s should dispatch", m)
		require.NotNil(t, res, "stub %s should return payload", m)
	}
}

// ──────────────────────────── get_conf ────────────────────────────

// TestHandleHeartbeatGetConf_成功 返回服务配置三键。
func TestHandleHeartbeatGetConf_成功(t *testing.T) {
	d := newHeartbeatTestDispatcher(&fakeHeartbeatSvc{})
	res, err := d.Dispatch("heartbeat.get_conf", map[string]any{}, "")
	require.NoError(t, err)
	assert.Equal(t, 60.0, res["every"])
}

// TestHandleHeartbeatGetConf_服务nil SERVICE_UNAVAILABLE。
func TestHandleHeartbeatGetConf_服务nil(t *testing.T) {
	// 直接构造 handler nil 服务（注册分支不覆盖此路径，防御保留）
	h := handleHeartbeatGetConf(nil)
	res, err := h(context.Background(), nil, "")
	require.NoError(t, err)
	assert.False(t, res["ok"].(bool))
	assert.Equal(t, WsErrServiceUnavailable, res["code"])
}

// ──────────────────────────── set_conf ────────────────────────────

// TestHandleHeartbeatSetConf_params非对象 BAD_REQUEST。
func TestHandleHeartbeatSetConf_params非对象(t *testing.T) {
	d := newHeartbeatTestDispatcher(&fakeHeartbeatSvc{})
	res, err := d.Dispatch("heartbeat.set_conf", nil, "")
	require.NoError(t, err)
	assert.False(t, res["ok"].(bool))
	assert.Equal(t, WsErrBadRequest, res["code"])
}

// TestHandleHeartbeatSetConf_every非法 BAD_REQUEST 载荷。
func TestHandleHeartbeatSetConf_every非法(t *testing.T) {
	hb := &fakeHeartbeatSvc{setErr: assert.AnError}
	d := newHeartbeatTestDispatcher(hb)
	res, err := d.Dispatch("heartbeat.set_conf", map[string]any{"every": -1}, "")
	require.NoError(t, err)
	assert.False(t, res["ok"].(bool))
	assert.Equal(t, WsErrBadRequest, res["code"])
}

// TestHandleHeartbeatSetConf_every字符串数字 "60" 经 heartbeatAnyToFloat 转 60.0。
func TestHandleHeartbeatSetConf_every字符串数字(t *testing.T) {
	hb := &fakeHeartbeatSvc{}
	d := newHeartbeatTestDispatcher(hb)
	res, err := d.Dispatch("heartbeat.set_conf", map[string]any{"every": "60"}, "")
	require.NoError(t, err)
	require.NotNil(t, hb.lastEvery)
	assert.Equal(t, 60.0, *hb.lastEvery)
	assert.Equal(t, 60.0, res["every"])
}

// TestHandleHeartbeatSetConf_every非法类型 bool 非 Python float 可转类型 → BAD_REQUEST。
// Python float(True)=1.0 但 web 层 Go 侧 anyToFloat 面向 string/int/int64/float64
// （对齐清单 float() 隐式转换逐类型核对后收窄面，bool 不在其列）。
func TestHandleHeartbeatSetConf_every非法类型(t *testing.T) {
	hb := &fakeHeartbeatSvc{}
	d := newHeartbeatTestDispatcher(hb)
	res, err := d.Dispatch("heartbeat.set_conf", map[string]any{"every": true}, "")
	require.NoError(t, err)
	assert.False(t, res["ok"].(bool))
	assert.Contains(t, res["error"], "every must be number")
}

// TestHandleHeartbeatSetConf_activeHours清除 传 {}（缺 start/end）→ nil 清除语义。
func TestHandleHeartbeatSetConf_activeHours清除(t *testing.T) {
	hb := &fakeHeartbeatSvc{}
	d := newHeartbeatTestDispatcher(hb)
	_, err := d.Dispatch("heartbeat.set_conf", map[string]any{
		"active_hours": map[string]any{},
	}, "")
	require.NoError(t, err)
	assert.Nil(t, hb.lastAH, "empty active_hours should normalize to nil (clear)")
}

// TestHandleHeartbeatSetConf_activeHours非法类型 非数组非 map → nil。
func TestHandleHeartbeatSetConf_activeHours非法类型(t *testing.T) {
	hb := &fakeHeartbeatSvc{}
	d := newHeartbeatTestDispatcher(hb)
	_, err := d.Dispatch("heartbeat.set_conf", map[string]any{
		"active_hours": "not-a-map",
	}, "")
	require.NoError(t, err)
	assert.Nil(t, hb.lastAH)
}

// TestHandleHeartbeatSetConf_activeHours数值直通 数值 1320 原样传入服务层
// （不字符串化——保 NormalizeActiveHours 回转链）。
func TestHandleHeartbeatSetConf_activeHours数值直通(t *testing.T) {
	hb := &fakeHeartbeatSvc{}
	d := newHeartbeatTestDispatcher(hb)
	_, err := d.Dispatch("heartbeat.set_conf", map[string]any{
		"active_hours": map[string]any{"start": 1320, "end": float64(360)},
	}, "")
	require.NoError(t, err)
	require.NotNil(t, hb.lastAH, "start/end present should pass through")
	assert.Equal(t, 1320, hb.lastAH["start"])
	assert.Equal(t, float64(360), hb.lastAH["end"])
}

// TestHandleHeartbeatSetConf_成功写回 config.yaml（SetRoundTrip 保注释）。
func TestHandleHeartbeatSetConf_成功写回(t *testing.T) {
	tmpDir := t.TempDir()
	// ConfigDir() = WorkspaceDir()/config（getResolvedPaths：userConfigDir 存在才不回退）
	cfgDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	orig := "# 顶部注释保留验证\nheartbeat:\n  every: 30  # 行注释\n  target: web\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(orig), 0o644))
	// config.New("") 走 pathutil.ConfigFile()——UAPCLAW_DATA_DIR 重定向 WorkspaceDir
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())

	hb := &fakeHeartbeatSvc{}
	d := newHeartbeatTestDispatcher(hb)
	res, err := d.Dispatch("heartbeat.set_conf", map[string]any{
		"every": 120, "target": "tui",
		"active_hours": map[string]any{"start": "08:00", "end": "22:00"},
	}, "")
	require.NoError(t, err)
	// fake 已应用更新：响应与写回值一致
	assert.Equal(t, 120.0, res["every"])
	assert.Equal(t, "tui", res["target"])

	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "# 顶部注释保留验证") // 头注释保留
	assert.Contains(t, content, "every: 120")      // 数值更新
	assert.Contains(t, content, "target: tui")     // 目标更新
	// yaml.v3 对 "08:00" 字符串加引号防 60 进制歧义（与 Python ruamel 引号风格差异可接受）
	assert.Contains(t, content, `start: "08:00"`) // active_hours 写入
}

// TestHandleHeartbeatSetConf_agentClientNil跳过清缓存 无 panic 即通过。
func TestHandleHeartbeatSetConf_agentClientNil跳过清缓存(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())

	hb := &fakeHeartbeatSvc{}
	d := newHeartbeatTestDispatcherWithClient(hb, nil)
	_, err := d.Dispatch("heartbeat.set_conf", map[string]any{"every": 90}, "")
	require.NoError(t, err)
}

// ──────────────────────────── get_path ────────────────────────────

// TestHandleHeartbeatGetPath_相对路径 返回 agent/workspace/HEARTBEAT.md。
func TestHandleHeartbeatGetPath_相对路径(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())

	d := newHeartbeatTestDispatcher(&fakeHeartbeatSvc{})
	res, err := d.Dispatch("heartbeat.get_path", map[string]any{}, "")
	require.NoError(t, err)
	path, ok := res["path"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasSuffix(path, "agent/workspace/HEARTBEAT.md"),
		"path should be relative, got %q", path)
	assert.False(t, strings.HasPrefix(path, "/"), "path should not be absolute")
}

// ──────────────────────────── session.list 过滤（11.11 偏差⑨） ────────────────────────────

// TestHandleSessionList_Heartbeat过滤 heartbeat_ 前缀会话目录不参与列表展示。
// 对齐 Python session_metadata.py L31 _HEARTBEAT_SESSION_PREFIX = "heartbeat_"。
// 注意非 heartbeat_ 前缀目录（如 dream7.20 sweeper 管辖的普通目录）不过滤。
func TestHandleSessionList_Heartbeat过滤(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())

	sessionsDir := workspace.AgentSessionsDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sessionsDir, "heartbeat_xxx_abc123"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(sessionsDir, "normal-session"), 0o755))

	result, err := handleSessionList(context.Background(), nil, "")
	require.NoError(t, err)
	sessions, _ := result["sessions"].([]map[string]any)
	for _, s := range sessions {
		sid, _ := s["session_id"].(string)
		assert.False(t, strings.HasPrefix(sid, "heartbeat_"),
			"heartbeat session should be filtered, got %q", sid)
	}
	assert.Len(t, sessions, 1, "only normal-session should remain")
}
