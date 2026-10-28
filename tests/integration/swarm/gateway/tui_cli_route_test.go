//go:build integration

package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	mh "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CliRouteFullChainSuite CLI 路由全链路往返集成测试套件。
//
// 对齐 Python test_cli_channel_ws.py（进程内变体：不 spawn 真实进程，
// mock 注入点在 llm.ClientRegistry 边界）。
// 全局单例规避：suite 生命周期内 7 件套只 Start/Stop 一次，
// 两个 mode 测试共享装配（GetAgent 缓存 key 含 mode，两实例共存无冲突）。
type CliRouteFullChainSuite struct {
	isuite.BaseIntegrationSuite
	// 7 件套（SetupSuite 构造，TearDownSuite 停止）
	inboundServer *gateway.InboundGatewayServer
	gwServer      *gateway.GatewayServer
	channelMgr    *channel_manager.ChannelManager
	msgHandler    *mh.MessageHandler
	agentClient   *routing.AgentClient
	transport     *transport.ChannelTransport
	agentServer   *server.AgentServer
	// gwPort GatewayServer 随机端口
	gwPort int
	// providerName mock provider 注册名
	providerName string
	// envBackup 环境变量备份（[key, 原值, 是否存在]，TearDown 恢复）
	envBackup [][3]string
	// modeRecorder 工厂 wrapper 记录的 (mode, subMode)
	modeRecorder struct {
		mu    sync.Mutex
		modes []string
	}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// TestCliRouteFullChainSuite 运行 CLI 路由全链路集成测试套件
func TestCliRouteFullChainSuite(t *testing.T) {
	suite.Run(t, new(CliRouteFullChainSuite))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 装配 7 件套并启动
func (s *CliRouteFullChainSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	// ── mock provider 注册（runner_suite.go 先例模式）──
	// 注意：必须在 setupDataDir 之前（config.yaml 要写入此 provider 名）
	s.providerName = fmt.Sprintf("mock_cli_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())

	// ── 环境三件套（forbidden_test.go 先例模式）──
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.setupDataDir())
	pathutil.ResetCache()

	// ── 7 件套构造（自底向上）──
	// 1. Transport + AgentServer + 工厂 wrapper（复刻 defaultAgentFactory + mode 记录）
	s.transport = transport.NewChannelTransport()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.agentServer = server.NewAgentServer(cfg, s.transport)
	s.agentServer.SetAgentFactoryForTest(s.recordingAgentFactory)

	// 2. AgentClient（消费同一 Transport）
	s.agentClient = routing.NewAgentClient(s.transport)

	// 3. MessageHandler 消息处理器
	s.msgHandler = mh.NewMessageHandler(s.agentClient)

	// 4. ChannelManager（注入 MessageHandler）
	s.channelMgr = channel_manager.NewChannelManager(s.msgHandler, nil, nil)

	// 5. InboundGatewayServer（NormalizeAndForwardMessage → ChannelManager）
	s.inboundServer = gateway.NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {
		gateway.NormalizeAndForwardMessage(msg, s.channelMgr)
	})

	// 6. GatewayServer（随机端口 + 生产 tui 路由装配）
	s.gwPort = s.pickFreePort()
	gwCfg := gateway.NewGatewayServerConfig()
	gwCfg.Host = "127.0.0.1"
	gwCfg.Port = s.gwPort
	gwCfg.Routes["/tui"] = gateway.BuildTuiRouteConfig(gateway.TuiRouteParams{
		MessageHandler: s.msgHandler,
	})
	s.gwServer = gateway.NewGatewayServer(gwCfg)
	s.gwServer.OnMessage(s.inboundServer.HandleMessage)

	// 7. 出站注册（tui → GatewayServer）
	s.channelMgr.RegisterExternalChannel("tui", s.gwServer)

	// ── Start 顺序（对齐 app_gateway.go）──
	s.Require().NoError(s.agentServer.Start(s.Ctx))
	s.Require().NoError(s.agentClient.Connect(s.Ctx))
	s.Require().NoError(s.msgHandler.StartForwarding(s.Ctx))
	s.Require().NoError(s.channelMgr.StartDispatch(s.Ctx))
	s.Require().NoError(s.inboundServer.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))
}

// TearDownSuite 逆序停止 + 清理
func (s *CliRouteFullChainSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.inboundServer != nil {
		s.inboundServer.Stop()
	}
	if s.channelMgr != nil {
		_ = s.channelMgr.StopDispatch()
	}
	if s.msgHandler != nil {
		_ = s.msgHandler.StopForwarding()
	}
	if s.agentClient != nil {
		s.agentClient.Disconnect()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	pathutil.ResetCache()
	s.restoreEnv()
	s.BaseIntegrationSuite.TearDownSuite()
}

// TestChatSendFullChain_显式agentPlan 显式 mode=agent.plan 的全链路往返。
//
// 对齐 Python test_cli_route_system_roundtrip 的 chat_res 断言 +
// Go 侧完整事件序列断言。
func (s *CliRouteFullChainSuite) TestChatSendFullChain_显式agentPlan() {
	// mock 预设：单文本 chunk（队列耗尽坑预防——每测试重置）
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("CLI 全链路响应：agent.plan"))
	s.modeRecorder.mu.Lock()
	s.modeRecorder.modes = nil
	s.modeRecorder.mu.Unlock()

	s.assertCommonRoundtrip("req-agent-plan", "sess_agent_plan", "agent.plan")

	// 断言③：mock 命中（方法体内直接断言，同时满足 integration-audit
	// 有效测试判定——共享 helper 内的断言不在本方法 AST 内）
	s.Require().GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1,
		"MockModelClient 未被调用——config.yaml 的 client_provider 未命中")
	// 断言④：mode 注入验证（显式 agent.plan）
	s.Require().Equal("agent/plan", s.lastRecordedMode(),
		"工厂应收到的 mode=agent/subMode=plan，实际: %s", s.lastRecordedMode())
}

// TestChatSendFullChain_默认codeNormal 不传 mode，验证 tui 路由默认注入 code.normal。
//
// 这是 gateway_server.go L508-522 setdefault 逻辑的直接断言：
// 若注入损坏，工厂将收到 agent/plan（AgentServer 侧 fallback），断言失败。
func (s *CliRouteFullChainSuite) TestChatSendFullChain_默认codeNormal() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("CLI 全链路响应：code.normal"))
	s.modeRecorder.mu.Lock()
	s.modeRecorder.modes = nil
	s.modeRecorder.mu.Unlock()

	s.assertCommonRoundtrip("req-code-normal", "sess_code_normal", "")

	// 断言③：mock 命中（方法体内直接断言，同时满足 integration-audit
	// 有效测试判定——共享 helper 内的断言不在本方法 AST 内）
	s.Require().GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1,
		"MockModelClient 未被调用——config.yaml 的 client_provider 未命中")
	// 断言④：tui 默认 mode 注入的直接证据
	s.Require().Equal("code/normal", s.lastRecordedMode(),
		"工厂应收到的 mode=code/subMode=normal（tui 默认注入），实际: %s", s.lastRecordedMode())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordingAgentFactory 复刻 runtime.defaultAgentFactory（agent_manager.go），
// 仅追加 (mode, subMode) 记录。若与 defaultAgentFactory 漂移须同步修订。
//
// mode 记录是 tui 默认 mode 注入（gateway_server.go L508-522 setdefault
// code.normal）的直接断言证据。
func (s *CliRouteFullChainSuite) recordingAgentFactory(config map[string]any, mode, subMode string) (*runtime.UapClaw, error) {
	s.modeRecorder.mu.Lock()
	s.modeRecorder.modes = append(s.modeRecorder.modes, mode+"/"+subMode)
	s.modeRecorder.mu.Unlock()

	agent := runtime.NewUapClaw()
	if err := agent.CreateInstance(
		runtime.WithCreateInstanceConfig(config),
		runtime.WithCreateInstanceMode(mode),
		runtime.WithCreateInstanceSubMode(subMode),
	); err != nil {
		return nil, fmt.Errorf("[AgentManager] 创建 Agent 实例失败: %w", err)
	}
	return agent, nil
}

// lastRecordedMode 返回最近一次记录的 mode/subMode
func (s *CliRouteFullChainSuite) lastRecordedMode() string {
	s.modeRecorder.mu.Lock()
	defer s.modeRecorder.mu.Unlock()
	if len(s.modeRecorder.modes) == 0 {
		return ""
	}
	return s.modeRecorder.modes[len(s.modeRecorder.modes)-1]
}

// setupDataDir 构造 UAPCLAW_DATA_DIR 三件套（config/config.yaml + workspace 目录）
func (s *CliRouteFullChainSuite) setupDataDir() string {
	dataDir := s.T().TempDir()
	configDir := filepath.Join(dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(dataDir, "agent", "workspace"), 0o755))

	// 注意两点（对齐 buildModelCacheFromDefaults / NewModelClientConfig 校验）：
	// 1. model_name 必须在 model_client_config 内层——mcc["model_name"] 取值，
	//    写在外层条目会被跳过导致 mock 不命中；
	// 2. api_base 与 client_id 为必填校验项——缺失时条目被判无效跳过
	//    （"skipping invalid model entry"），model 落空后 agent 侧报
	//    client_id 为空的 LLM 初始化失败。
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true
`, s.providerName, s.providerName)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yamlContent), 0o644))
	return dataDir
}

// pickFreePort 选取随机可用端口（对齐 Python _pick_free_port）
func (s *CliRouteFullChainSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录原值并设置环境变量（TearDown 恢复）
func (s *CliRouteFullChainSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	s.envBackup = append(s.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量
func (s *CliRouteFullChainSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "false" {
			_ = os.Unsetenv(e[0])
		} else {
			_ = os.Setenv(e[0], e[1])
		}
	}
}

// dialWS 连接 /tui 路由
func (s *CliRouteFullChainSuite) dialWS() *websocket.Conn {
	url := fmt.Sprintf("ws://127.0.0.1:%d/tui", s.gwPort)
	var conn *websocket.Conn
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, _, err = websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			return conn
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.T().Fatalf("连接 %s 失败: %v", url, err)
	return nil
}

// sendChatSend 发送 chat.send 请求
func (s *CliRouteFullChainSuite) sendChatSend(conn *websocket.Conn, reqID, sessionID, mode string) {
	params := map[string]any{
		"session_id": sessionID,
		"content":    "hello cli",
	}
	if mode != "" {
		params["mode"] = mode
	}
	frame := map[string]any{
		"type":   "req",
		"id":     reqID,
		"method": "chat.send",
		"params": params,
	}
	data, err := json.Marshal(frame)
	s.Require().NoError(err)
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, data))
}

// readFrameUntil 读帧直到谓词命中或超时，返回命中帧
func (s *CliRouteFullChainSuite) readFrameUntil(conn *websocket.Conn, timeout time.Duration, pred func(map[string]any) bool) map[string]any {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		_, data, err := conn.ReadMessage()
		if err != nil {
			s.T().Fatalf("读帧失败: %v", err)
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		if pred(frame) {
			return frame
		}
	}
	s.T().Fatal("超时：未等到目标帧")
	return nil
}

// eventSequence 收集本 session 的事件帧序列（chat.final 或 processing_status(false) 后停止）。
//
// session 过滤（双模式测试隔离）：断连取消是异步传播的——前一测试 conn.Close() 触发
// CancelAgentSessionsOnDisconnect，其 chat.interrupt_result 事件到达时旧连接已不可写，
// 出站按 channel 广播 fallback（Python 语义），会落到同 channel 的下一个测试连接上。
// 故只收集 payload.session_id 匹配本测试 session 的帧，其余跳过（不能提前 return——
// 后续帧仍属本 session，须继续读取）。
func (s *CliRouteFullChainSuite) eventSequence(conn *websocket.Conn, timeout time.Duration, sessionID string) []map[string]any {
	var events []map[string]any
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		if frame["type"] != "event" {
			continue
		}
		// session_id 过滤：不匹配的帧（跨测试串扰）直接丢弃
		if payload, ok := frame["payload"].(map[string]any); ok {
			if sid, _ := payload["session_id"].(string); sid != sessionID {
				continue
			}
		}
		events = append(events, frame)
		ev, _ := frame["event"].(string)
		if ev == "chat.processing_status" {
			if payload, ok := frame["payload"].(map[string]any); ok {
				if isProc, _ := payload["is_processing"].(bool); !isProc {
					return events // 序列终点
				}
			}
		}
	}
	return events
}

// eventNames 提取事件名序列
func eventNames(events []map[string]any) []string {
	names := make([]string, 0, len(events))
	for _, e := range events {
		names = append(names, fmt.Sprintf("%v", e["event"]))
	}
	return names
}

// findEvent 在序列中找第一个指定事件
func findEvent(events []map[string]any, name string) map[string]any {
	for _, e := range events {
		if e["event"] == name {
			return e
		}
	}
	return nil
}

// assertCommonRoundtrip 主/副测试共用的完整往返断言（对齐 Python + Go 链路事件序列基准）。
// mode 为空串表示不传 mode 字段（副测试验证 tui 默认注入）。
func (s *CliRouteFullChainSuite) assertCommonRoundtrip(reqID, sessionID, mode string) {
	conn := s.dialWS()
	defer conn.Close()

	s.sendChatSend(conn, reqID, sessionID, mode)

	// 断言①：ack 帧（对齐 Python 原断言）
	ack := s.readFrameUntil(conn, 10*time.Second, func(f map[string]any) bool {
		return f["type"] == "res" && f["id"] == reqID
	})
	s.Require().Equal(true, ack["ok"], "ack ok 应为 true")
	payload, ok := ack["payload"].(map[string]any)
	s.Require().True(ok, "ack payload 应为 map")
	s.Require().Equal(true, payload["accepted"], "accepted 应为 true")
	s.Require().Equal(sessionID, payload["session_id"], "session_id 应为 %s", sessionID)

	// 断言②：完整事件序列
	events := s.eventSequence(conn, 30*time.Second, sessionID)
	names := eventNames(events)

	// 断言④前置：mock 命中验证（防 config.yaml 错误静默 fallback 真实 LLM）。
	// 注意：须在事件序列收集后检查——ack 帧由 tui 本地 handler 立即返回，
	// 而 agent 流异步执行，ack 时刻 LLM 尚未被调用。
	s.Require().GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1, "MockModelClient 未被调用——config.yaml 的 client_provider 未命中")
	// 首帧 processing_status(true)（gateway 侧发出）
	s.Require().NotEmpty(names, "应收到至少一个事件帧")
	s.Require().Equal("chat.processing_status", names[0], "首事件应为 processing_status(true)，实际: %v", names)
	if p, ok := events[0]["payload"].(map[string]any); ok {
		s.Require().Equal(true, p["is_processing"], "首事件 is_processing 应为 true")
	}
	// 尾帧 processing_status(false)
	s.Require().Equal("chat.processing_status", names[len(names)-1], "尾事件应为 processing_status(false)，实际: %v", names)
	if p, ok := events[len(events)-1]["payload"].(map[string]any); ok {
		s.Require().Equal(false, p["is_processing"], "尾事件 is_processing 应为 false")
	}
	// delta 在 final 前（顺序断言核心）
	deltaIdx, finalIdx := -1, -1
	for i, n := range names {
		if n == "chat.delta" && deltaIdx == -1 {
			deltaIdx = i
		}
		if n == "chat.final" && finalIdx == -1 {
			finalIdx = i
		}
	}
	s.Require().NotEqual(-1, deltaIdx, "缺少 chat.delta 事件: %v", names)
	s.Require().NotEqual(-1, finalIdx, "缺少 chat.final 事件: %v", names)
	s.Require().Less(deltaIdx, finalIdx, "chat.delta 应在 chat.final 之前: %v", names)
	// usage_metadata 存在（值不断言）
	s.Require().NotNil(findEvent(events, "chat.usage_metadata"), "缺少 chat.usage_metadata: %v", names)
	// usage_summary 不应出现（TotalTokens=0）
	s.Require().Nil(findEvent(events, "chat.usage_summary"), "不应出现 chat.usage_summary: %v", names)

	// 断言④：delta/final 内容 = mock 预设文本。
	// llm_output 产出的 chat.delta content 为字符串（mock 文本）；
	// controller_output 透传的 chat.delta content 为 map（合法事件，跳过）。
	if d := findEvent(events, "chat.delta"); d != nil {
		if p, ok := d["payload"].(map[string]any); ok {
			if c, ok := p["content"].(string); ok {
				s.Require().Contains(c, "CLI 全链路响应", "delta 内容应为 mock 预设文本")
			}
		}
	}
	if f := findEvent(events, "chat.final"); f != nil {
		if p, ok := f["payload"].(map[string]any); ok {
			s.Require().Contains(p["content"], "CLI 全链路响应", "final 内容应为 mock 预设文本")
		}
	}
}
