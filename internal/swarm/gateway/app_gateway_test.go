package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	gwa2a "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/protocol/a2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// newTestAppGatewayServer 创建测试用 AppGatewayServer
func newTestAppGatewayServer(t *testing.T) *AppGatewayServer {
	t.Helper()
	cfg, err := config.New("")
	require.NoError(t, err)
	transport := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(transport)
	gs, err := NewAppGatewayServer(cfg, agentClient)
	require.NoError(t, err)
	return gs
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewAppGatewayServer(t *testing.T) {
	gs := newTestAppGatewayServer(t)
	assert.NotNil(t, gs)
	assert.NotNil(t, gs.router)
	assert.NotNil(t, gs.webChannel)
	assert.NotNil(t, gs.channelMgr)
	assert.NotNil(t, gs.msgHandler)
}

// TestNewAppGatewayServer_装配全局Config RegisterConfig 幂等装配
func TestNewAppGatewayServer_装配全局Config(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgFile, []byte("gateway:\n  session_map_scope: per_chat_bot_user\n"), 0o644)

	hookscfg.ResetGlobalConfigForTest()
	defer hookscfg.ResetGlobalConfigForTest()

	cfg, err := config.New(cfgFile)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}

	transport := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(transport)

	s, err := NewAppGatewayServer(cfg, agentClient)
	if err != nil {
		t.Fatalf("NewAppGatewayServer: %v", err)
	}
	assert.NotNil(t, s)

	// 全局 config 已注册（scope 可读到 per_chat_bot_user）
	if got := hookscfg.GetGlobalConfigForScope(); got == nil {
		t.Fatal("NewAppGatewayServer 应装配全局 Config")
	}
	if got := routing.LoadSessionMapScope(); got != routing.SessionMapScopePerChatBotUser {
		t.Errorf("装配后 scope 应读到 per_chat_bot_user，实际 %d", got)
	}
	// MessageHandler 的 hookHandler 非 nil
	if s.msgHandler == nil || s.msgHandler.GetGatewayHookHandlerForTest() == nil {
		t.Error("装配后 MessageHandler.gatewayHookHandler 应非 nil")
	}
}

func TestAppGatewayServer_Router路由(t *testing.T) {
	gs := newTestAppGatewayServer(t)

	// 启动测试服务器
	server := httptest.NewServer(gs.router)
	defer server.Close()

	client := server.Client()
	client.Timeout = 2 * time.Second

	// 测试 /file-api 端点
	resp, err := client.Get(server.URL + "/file-api/ws-debug-config")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// 测试前端静态文件
	resp, err = client.Get(server.URL + "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestAppGatewayServer_Stop(t *testing.T) {
	gs := newTestAppGatewayServer(t)

	// 验证 Stop 不崩溃（httpServer 为 nil 的情况）
	err := gs.Stop()
	assert.NoError(t, err)
}

func TestSPAHandler(t *testing.T) {
	handler := newSPAHandler(frontendDist)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()
	client.Timeout = 2 * time.Second

	// 测试根路径
	resp, err := client.Get(server.URL + "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// 测试 index.html
	resp, err = client.Get(server.URL + "/index.html")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestDefaultHostPort(t *testing.T) {
	// 空配置使用默认值
	assert.Equal(t, "127.0.0.1", defaultHost(nil))
	assert.Equal(t, 19000, defaultPort(nil))

	// 配置中的值
	cfg, _ := config.New("")
	assert.Equal(t, "127.0.0.1", defaultHost(cfg))
	assert.Equal(t, 19000, defaultPort(cfg))
}

func TestDefaultHost_环境变量优先(t *testing.T) {
	_ = os.Setenv("UAPCLAW_GATEWAY_HOST", "0.0.0.0")
	defer func() { _ = os.Unsetenv("UAPCLAW_GATEWAY_HOST") }()
	assert.Equal(t, "0.0.0.0", defaultHost(nil))
}

func TestDefaultPort_环境变量优先(t *testing.T) {
	_ = os.Setenv("UAPCLAW_GATEWAY_PORT", "8080")
	defer func() { _ = os.Unsetenv("UAPCLAW_GATEWAY_PORT") }()
	assert.Equal(t, 8080, defaultPort(nil))
}

func TestDefaultPort_环境变量无效(t *testing.T) {
	_ = os.Setenv("UAPCLAW_GATEWAY_PORT", "not-a-number")
	defer func() { _ = os.Unsetenv("UAPCLAW_GATEWAY_PORT") }()
	assert.Equal(t, 19000, defaultPort(nil))
}

func TestDefaultPort_环境变量零值(t *testing.T) {
	_ = os.Setenv("UAPCLAW_GATEWAY_PORT", "0")
	defer func() { _ = os.Unsetenv("UAPCLAW_GATEWAY_PORT") }()
	assert.Equal(t, 19000, defaultPort(nil))
}

func TestAppGatewayServer_StartStop(t *testing.T) {
	gs := newTestAppGatewayServer(t)

	err := gs.Start(context.Background())
	require.NoError(t, err)

	err = gs.Stop()
	assert.NoError(t, err)
}

func TestAppGatewayServer_OnConfigSaved(t *testing.T) {
	gs := newTestAppGatewayServer(t)
	callback := gs.OnConfigSaved()
	assert.NotNil(t, callback)
}

// TestAppGatewayServer_空路由装配不panic 11.9 装配链验证 + 11.15 /tui 最小装配：
// Start 全流程（inboundServer + gwServer 装配 + Routes["/tui"] 填充 + tui 出站注册）
// → Stop 逆序。GATEWAY_PORT 用空闲端口 19099（t.Setenv 结束自动还原；0 会被
// Atoi 的 p>0 拒绝）。
func TestAppGatewayServer_空路由装配不panic(t *testing.T) {
	t.Setenv("GATEWAY_HOST", "127.0.0.1")
	t.Setenv("GATEWAY_PORT", "19099")
	gs := newTestAppGatewayServer(t)

	require.NoError(t, gs.Start(context.Background()))
	t.Cleanup(func() { _ = gs.Stop() })

	// 装配断言：gwServer/inboundServer 均已创建
	assert.NotNil(t, gs.gwServer)
	assert.NotNil(t, gs.inboundServer)
	// gwServer 读到环境变量端口
	assert.Equal(t, 19099, gs.gwServer.config.Port)
	assert.True(t, gs.gwServer.running.Load())
	assert.True(t, gs.inboundServer.running.Load())
	// 11.15 最小装配：/tui CLI 路由已填充（对齐 Python build_cli_route_binding）
	tuiRoute, ok := gs.gwServer.config.Routes["/tui"]
	if assert.True(t, ok, "Routes 应含 /tui") {
		assert.Equal(t, "tui", tuiRoute.ChannelID)
		assert.NotEmpty(t, tuiRoute.LocalHandlers)
		assert.NotNil(t, tuiRoute.DisconnectHandler)
	}
	// tui 出站渠道已注册
	assert.NotNil(t, gs.channelMgr.GetExternalChannel("tui"))
}

// ──────────────────────────── 11.11 Heartbeat 装配 ────────────────────────────

// TestNewAppGatewayServer_Heartbeat装配 默认构造心跳服务，三级回退到默认 60s/web。
func TestNewAppGatewayServer_Heartbeat装配(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	os.Unsetenv("HEARTBEAT_INTERVAL")         //nolint:errcheck // 测试中和
	os.Unsetenv("HEARTBEAT_TIMEOUT")          //nolint:errcheck // 测试中和
	os.Unsetenv("HEARTBEAT_RELAY_CHANNEL_ID") //nolint:errcheck // 测试中和
	workspace.SetUserHome(workspace.UserHomeDir())
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config", "config.yaml"), []byte(""), 0o644))

	cfg, err := config.New("")
	require.NoError(t, err)
	tp := transport.NewChannelTransport()
	ac := routing.NewAgentClient(tp)
	gs, err := NewAppGatewayServer(cfg, ac)
	require.NoError(t, err)
	require.NotNil(t, gs.heartbeatService)
	// 未设 env 且 config 无 heartbeat 段 → 默认 60s
	assert.Equal(t, 60.0, gs.heartbeatService.GetHeartbeatConf()["every"])
	// 默认 relay target "web"
	assert.Equal(t, "web", gs.heartbeatService.GetHeartbeatConf()["target"])
}

// TestNewAppGatewayServer_HeartbeatEnv回退 HEARTBEAT_INTERVAL env 优先于 config 与默认。
func TestNewAppGatewayServer_HeartbeatEnv回退(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	os.Unsetenv("HEARTBEAT_TIMEOUT")          //nolint:errcheck // 测试中和
	os.Unsetenv("HEARTBEAT_RELAY_CHANNEL_ID") //nolint:errcheck // 测试中和
	workspace.SetUserHome(workspace.UserHomeDir())
	cfgDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	// config.every=42 应被 env=15 覆盖（env > config > 60 三级回退）
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"),
		[]byte("heartbeat:\n  every: 42\n"), 0o644))
	t.Setenv("HEARTBEAT_INTERVAL", "15")

	cfg, err := config.New("")
	require.NoError(t, err)
	tp := transport.NewChannelTransport()
	ac := routing.NewAgentClient(tp)
	gs, err := NewAppGatewayServer(cfg, ac)
	require.NoError(t, err)
	require.NotNil(t, gs.heartbeatService)
	assert.Equal(t, 15.0, gs.heartbeatService.GetHeartbeatConf()["every"])
}

// TestNewAppGatewayServer_HeartbeatConfig回退 config.yaml heartbeat.every 次优先于默认。
func TestNewAppGatewayServer_HeartbeatConfig回退(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	os.Unsetenv("HEARTBEAT_INTERVAL")         //nolint:errcheck // 测试中和
	os.Unsetenv("HEARTBEAT_TIMEOUT")          //nolint:errcheck // 测试中和
	os.Unsetenv("HEARTBEAT_RELAY_CHANNEL_ID") //nolint:errcheck // 测试中和
	workspace.SetUserHome(workspace.UserHomeDir())
	cfgDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	cfgContent := "heartbeat:\n  every: 42\n  target: tui\n"
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgContent), 0o644))

	cfg, err := config.New("")
	require.NoError(t, err)
	tp := transport.NewChannelTransport()
	ac := routing.NewAgentClient(tp)
	gs, err := NewAppGatewayServer(cfg, ac)
	require.NoError(t, err)
	require.NotNil(t, gs.heartbeatService)
	conf := gs.heartbeatService.GetHeartbeatConf()
	assert.Equal(t, 42.0, conf["every"])
	assert.Equal(t, "tui", conf["target"])
}

// TestNewAppGatewayServer_IM管线装配 11.12 双管线非 nil。
func TestNewAppGatewayServer_IM管线装配(t *testing.T) {
	gs := newTestAppGatewayServer(t)
	assert.NotNil(t, gs.imInbound)
	assert.NotNil(t, gs.imOutbound)
}

// ──────────────────────────── 11.25 ACP 装配测试 ────────────────────────────

// TestAppGateway_AcpRouteAssembled /acp 路由 + 桥 + 外部渠道注册（⑥ 装配验证）。
// gwServer 装配在 Start 内（对齐 Python run() 启动序）——先起后断言再停。
func TestAppGateway_AcpRouteAssembled(t *testing.T) {
	gs := newTestAppGatewayServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = gs.Start(ctx) }()
	// 等待 gwServer 装配完成：Start 内 connectWithRetry 等待 connection.ack
	// 超时 5s 后继续（ChannelTransport 测试态无 AgentServer 对端），轮询上限 8s。
	// 完成信号 = /acp 路由拦截器已装填（InstallDefaultAcpHooks 是装配序的最后
	// 阶段之一；routes 经 clientsMu 保护，读侧同锁避免与 hooks 装填竞态）
	deadline := time.Now().Add(8 * time.Second)
	var gws *GatewayServer
	for time.Now().Before(deadline) {
		gws = gs.GetGatewayServer()
		if gws != nil {
			gws.clientsMu.RLock()
			r := gws.config.Routes["/acp"]
			done := r != nil && r.InboundInterceptor != nil && r.OutboundInterceptor != nil
			gws.clientsMu.RUnlock()
			if done {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if gws == nil {
		t.Fatal("gwServer not assembled within 8s")
	}

	// 1. /acp 路由存在且渠道为 acp（同包直读——字段未导出但同包可见）
	route := gws.config.Routes["/acp"]
	if route == nil {
		t.Fatal("/acp route should be assembled")
	}
	if route.ChannelID != "acp" {
		t.Fatalf("/acp channel = %q", route.ChannelID)
	}
	// 2. forward 集合与 web 渠道一致（_FORWARD_REQ_METHODS）
	if !route.ForwardMethods["chat.send"] {
		t.Fatal("/acp should forward chat.send")
	}
	// 3. 拦截器已装（InstallDefaultAcpHooks 已在装配内调用）
	if route.InboundInterceptor == nil || route.OutboundInterceptor == nil {
		t.Fatal("acp route interceptors should be installed")
	}
	// 4. acp 外部渠道注册（RegisterExternalChannel("acp", gwServer)，
	// 经导出查询方法 GetExternalChannel 验证）
	if gs.channelMgr.GetExternalChannel("acp") == nil {
		t.Fatal("acp external channel should be registered")
	}
	// 5. 桥已注入（同包直读 acpBridge 字段——Task 5 ① 装的字段）
	gws.clientsMu.Lock()
	bridgeSet := gws.acpBridge != nil
	gws.clientsMu.Unlock()
	if !bridgeSet {
		t.Fatal("acp bridge should be set")
	}
}

// TestNewAppGatewayServer_注册A2A通道 步骤 4.8：a2a 通道应注册进 ChannelManager
func TestNewAppGatewayServer_注册A2A通道(t *testing.T) {
	t.Setenv("A2A_SERVER_ENABLED", "")
	s := newTestAppGatewayServer(t)
	require.NotNil(t, s.a2aChannel, "a2aChannel 未装配")
	assert.NotNil(t, s.channelMgr.GetChannel("a2a"), "a2a 通道未注册进 ChannelManager")
	assert.Equal(t, "a2a", s.a2aChannel.ChannelID())
	assert.Equal(t, cm.ChannelTypeA2A, s.a2aChannel.ChannelType())
}

// TestNewAppGatewayServer_A2A默认关闭 未设 env 时 Enabled=false
func TestNewAppGatewayServer_A2A默认关闭(t *testing.T) {
	t.Setenv("A2A_SERVER_ENABLED", "")
	s := newTestAppGatewayServer(t)
	cfg, ok := s.a2aChannel.Config().(gwa2a.A2AChannelConfig)
	require.True(t, ok, "Config 类型 = %T", s.a2aChannel.Config())
	assert.False(t, cfg.Enabled, "未设 env 时应关闭")
}
