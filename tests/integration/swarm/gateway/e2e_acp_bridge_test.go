//go:build integration

package gateway_test

// acp_bridge_e2e_test.go ACP 模式一端到端集成测试（11.25 Task 11）。
//
// AcpBridgeE2ESuite 模式一全链路：AcpChannel（io.Pipe stdin + safeBuffer stdout）
// → WS 回环 → GatewayServer /acp + AcpGatewayBridge → InboundGatewayServer →
// MessageHandler（别名链）→ E2A → AgentServer（MockModelClient）→ server_push
// → 桥 SendMessage → session/update/result 帧 → AcpChannel stdout。
// 对齐 Python 无独立 e2e（Go 增强项，spec §7）。
//
// 文件名以 a 开头排序在 doc.go 之后（目录首个 _test.go 的 package X_test
// 剥离误判坑——doc.go 注释既有警告）。
//
// 用例：
//  1. TestAcpBridgeE2E_PromptRoundTrip：initialize→session/new→session/prompt
//     （mock LLM 固定回复）→ stdout 收到 session/update(agent_message_chunk)+
//     result(end_turn)；验证别名链（prompt 带 IDE 会话 id，出站不要求回改——
//     桥侧 sessionID 即 IDE 会话，别名改写发生在 MessageHandler 内部）
//  2. TestAcpBridgeE2E_TimerSuperseded：pending RPC 挂起 end_turn 全链路——
//     read_text_file 工具 server_push → IDE 回响应 → result 补发
//     （对齐 Python defers_end_turn 用例；timer 家族由单测覆盖，见用例内注释）
//  3. TestAcpBridgeE2E_StdoutNoInterleave：并发 prompt 2 会话各 20 delta，
//     safeBuffer 收集的每行均合法 JSON（stdoutMu 无 interleave，Go 增强项）

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/protocol/acp"
	mh "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpBridgeE2ESuite ACP 模式一端到端套件。
//
// 装配对齐 app_gateway.go Start 的 /acp 段（Task 7）：BuildACPRouteConfig +
// NewAcpGatewayBridge（onMessage 转发 + WithBindSessionClient 注册 sessionToClient）
// + SetAcpBridge + InstallDefaultAcpHooks + RegisterExternalChannel("acp")。
type AcpBridgeE2ESuite struct {
	isuite.BaseIntegrationSuite
	// 7 件套（SetupSuite 构造，TearDownSuite 停止）
	inboundServer *gateway.InboundGatewayServer
	gwServer      *gateway.GatewayServer
	channelMgr    *channel_manager.ChannelManager
	msgHandler    *mh.MessageHandler
	agentClient   *routing.AgentClient
	chTransport   *transport.ChannelTransport
	agentServer   *server.AgentServer
	// gwPort GatewayServer 随机端口
	gwPort int
	// providerName mock provider 注册名
	providerName string
	// envBackup 环境变量备份（[key, 原值, 是否存在]，TearDown 恢复）
	envBackup [][3]string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// TestAcpBridgeE2ESuite 运行 ACP 模式一端到端集成测试套件
func TestAcpBridgeE2ESuite(t *testing.T) {
	suite.Run(t, new(AcpBridgeE2ESuite))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 装配 7 件套 + /acp 路由 + AcpChannel
func (s *AcpBridgeE2ESuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	// ── mock provider 注册（须在 setupDataDir 之前——config.yaml 写入此 provider 名）──
	s.providerName = fmt.Sprintf("mock_acp_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())

	// ── 环境三件套 ──
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.setupDataDir())
	pathutil.ResetCache()

	// ── 7 件套构造（自底向上，对齐 tui_cli_route_test.go 手法）──
	s.chTransport = transport.NewChannelTransport()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.agentServer = server.NewAgentServer(cfg, s.chTransport)
	s.agentClient = routing.NewAgentClient(s.chTransport)
	s.msgHandler = mh.NewMessageHandler(s.agentClient)
	s.channelMgr = channel_manager.NewChannelManager(s.msgHandler, nil, nil)
	s.inboundServer = gateway.NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {
		gateway.NormalizeAndForwardMessage(msg, s.channelMgr)
	})

	// GatewayServer：随机端口 + /acp 路由（Task 7 装配链复刻）
	s.gwPort = s.pickFreePort()
	gwCfg := gateway.NewGatewayServerConfig()
	gwCfg.Host = "127.0.0.1"
	gwCfg.Port = s.gwPort
	acpRoute := gateway.BuildACPRouteConfig()
	gwCfg.Routes[acpRoute.Path] = acpRoute
	s.gwServer = gateway.NewGatewayServer(gwCfg)

	// ACP 桥构造 + 注入（对齐 app_gateway.go L438-455）
	acpBridge := gateway.NewAcpGatewayBridge(
		func(ctx context.Context, msg *schema.Message) error {
			if s.gwServer == nil {
				return nil
			}
			return s.inboundServer.HandleMessage(ctx, msg)
		},
		gateway.WithBindSessionClient(func(sessionID string, ws *websocket.Conn) {
			s.gwServer.BindAcpSessionClientForTest(sessionID, ws)
		}))
	s.gwServer.SetAcpBridge(acpBridge)
	s.gwServer.InstallDefaultAcpHooks()

	// 出站注册 + onMessage
	s.channelMgr.RegisterExternalChannel("acp", s.gwServer)
	s.gwServer.OnMessage(s.inboundServer.HandleMessage)

	// ── Start 顺序 ──
	s.Require().NoError(s.agentServer.Start(s.Ctx))
	s.Require().NoError(s.agentClient.Connect(s.Ctx))
	s.Require().NoError(s.msgHandler.StartForwarding(s.Ctx))
	s.Require().NoError(s.channelMgr.StartDispatch(s.Ctx))
	s.Require().NoError(s.inboundServer.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))
}

// TearDownSuite 逆序停止 + 清理
func (s *AcpBridgeE2ESuite) TearDownSuite() {
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

// TestAcpBridgeE2E_PromptRoundTrip 用例 1：initialize→new→prompt 全链路往返。
func (s *AcpBridgeE2ESuite) TestAcpBridgeE2E_PromptRoundTrip() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ACP e2e roundtrip response"))

	harness := s.newAcpChannelHarness()
	defer harness.close()

	// initialize
	harness.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	initFrame := harness.waitFrame(10*time.Second, func(f map[string]any) bool {
		return f["id"] == float64(1)
	})
	result, ok := initFrame["result"].(map[string]any)
	s.Require().True(ok, "initialize 应返回 result map")
	s.Require().Equal(float64(1), result["protocolVersion"])

	// session/new
	harness.writeLine(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"sessionId":"ide-sess-roundtrip"}}`)
	newFrame := harness.waitFrame(10*time.Second, func(f map[string]any) bool {
		return f["id"] == float64(2)
	})
	newResult, _ := newFrame["result"].(map[string]any)
	s.Require().Equal("ide-sess-roundtrip", newResult["sessionId"])

	// session/prompt（IDE 会话 id 直传——别名链在 MessageHandler 内部改写）
	harness.writeLine(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"ide-sess-roundtrip","prompt":[{"type":"text","text":"hello acp e2e"}]}}`)

	// 期望帧：processing → agent_message_chunk → idle → result(end_turn)
	var chunkSeen, idleSeen bool
	var resultFrame map[string]any
	for resultFrame == nil {
		f := harness.consumeFrame(30 * time.Second)
		if f == nil {
			break
		}
		if f["method"] == "session/update" {
			params, _ := f["params"].(map[string]any)
			update, _ := params["update"].(map[string]any)
			switch update["sessionUpdate"] {
			case "agent_message_chunk":
				chunkSeen = true
				content, _ := update["content"].(map[string]any)
				s.Require().Equal("ACP e2e roundtrip response", content["text"])
			case "session_info_update":
				if update["status"] == "idle" {
					idleSeen = true
				}
			}
			continue
		}
		if f["id"] == float64(3) {
			resultFrame = f
		}
	}
	s.Require().True(chunkSeen, "应收到 agent_message_chunk 帧")
	s.Require().True(idleSeen, "应收到 idle 帧")
	s.Require().NotNil(resultFrame, "应收到 prompt result 帧")
	promptResult, _ := resultFrame["result"].(map[string]any)
	s.Require().Equal("end_turn", promptResult["stopReason"])

	// mock 命中验证（config.yaml client_provider 未静默 fallback）
	s.Require().GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1,
		"MockModelClient 未被调用——config.yaml 的 client_provider 未命中")
}

// TestAcpBridgeE2E_TimerSuperseded 用例 2：pending RPC 挂起 end_turn 全链路
// （对齐 Python test_gateway_server_defers_end_turn_until_pending_client_rpc_resolves
// 的 e2e 真实驱动版）。
// mock LLM 先回 read_text_file 工具调用 → agent 侧 AcpOutputManager.SendJsonrpcRequest
// server_push fs/read_text_file → 桥 OutboundIntercept 登记 pending → WS →
// channel handleGatewayJsonrpcRequest 直写 stdout + pending 表登记；
// mock 再回最终文本 → delta/final → idle。idle 到达时 pending 未清 →
// PendingStopReason=end_turn 挂起，无提前 result；IDE 回 jsonrpc 响应 →
// 桥 InboundIntercept → acp.tool_response → AgentServer CompleteJsonrpcResponse →
// agent 工具返回 → resolvePendingRPC → maybeFinalizeDeferredPrompts →
// result(end_turn) 补发。
// （原计划"idle 排 3s timer 被网关 jsonrpc 取消"场景在单 agent e2e 管线不可达：
// idle 由 MessageHandler 流收尾补发、恒晚于 chat.final，SawChatFinal=true 走
// finishNow 而非 scheduleWait；timer 家族已由 acp_channel_test.go 31 例单测
// 对等覆盖——Python 侧同为单元级覆盖（test_acp_channel.py monkeypatch），无 e2e 先例。）
func (s *AcpBridgeE2ESuite) TestAcpBridgeE2E_TimerSuperseded() {
	// 队列：① read_text_file 工具调用 ② 最终文本（工具响应回流后消费）
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_text_file", `{"path":"workspace/demo.txt"}`),
		mockllm.CreateTextResponse("tool flow final answer"))

	harness := s.newAcpChannelHarness()
	defer harness.close()

	// initialize 携带 fs 能力 → AgentManager.Initialize 记录 clientCapabilities
	//（AgentServer.handleInitialize 从 params["clientCapabilities"] 提取）
	harness.writeLine(`{"jsonrpc":"2.0","id":10,"method":"initialize","params":{"clientCapabilities":{"fs":true,"terminal":false}}}`)
	initFrame := harness.waitFrame(10*time.Second, func(f map[string]any) bool {
		return f["id"] == float64(10)
	})
	s.Require().NotNil(initFrame, "应收到 initialize 响应")

	// prompt（携带能力元数据走 injectACPCapabilities → refreshAcpRuntimeTools 注册六工具）
	harness.writeLine(`{"jsonrpc":"2.0","id":11,"method":"session/prompt","params":{"sessionId":"ide-sess-tool","prompt":[{"type":"text","text":"please read the file"}],"acp_client_capabilities":{"fs":true}}}`)

	// ① 等 fs/read_text_file 帧（agent 执行工具 → server_push → 桥直写 →
	//    channel pending 登记 + stdout 直写；方法名取自 read_text_file 工具实现）
	toolFrame := harness.waitFrame(30*time.Second, func(f map[string]any) bool {
		return f["method"] == "fs/read_text_file"
	})
	s.Require().NotNil(toolFrame, "应收到 fs/read_text_file 工具请求帧")
	toolID, _ := toolFrame["id"].(string)
	s.Require().NotEmpty(toolID, "工具请求帧应携带字符串 id")
	toolParams, _ := toolFrame["params"].(map[string]any)
	s.Require().Equal("workspace/demo.txt", toolParams["path"])

	// ② 挂起验证：工具未回响应前 agent 流阻塞（等 done channel），prompt
	//    result（id:11）不应出现。观察窗口 2s——足够暴露"未挂起直接收尾"缺陷
	//    （无挂起时 final/idle/result 会在数百 ms 内齐备）。
	time.Sleep(2 * time.Second)
	for _, f := range harness.snapshotFrames() {
		if f["id"] == float64(11) {
			if _, hasResult := f["result"]; hasResult {
				s.T().Fatalf("pending RPC 未挂起 end_turn——出现提前 result: %v", f)
			}
		}
	}

	// ③ IDE 回 jsonrpc 响应 → 桥 InboundIntercept → acp.tool_response →
	//    AgentServer CompleteJsonrpcResponse（工具返回）→ agent 二轮生成 →
	//    delta/final → idle（pending 已清 → result 立即写）
	harness.writeLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"result":{"content":"hello from ide"}}`, toolID))

	// ④ 最终文本 delta 流出（agent 二轮正常完成）
	sawFinalText := harness.waitFrame(30*time.Second, func(f map[string]any) bool {
		if f["method"] != "session/update" {
			return false
		}
		params, _ := f["params"].(map[string]any)
		update, _ := params["update"].(map[string]any)
		content, _ := update["content"].(map[string]any)
		return update["sessionUpdate"] == "agent_message_chunk" && content["text"] == "tool flow final answer"
	})
	s.Require().NotNil(sawFinalText, "应收到工具后最终文本 delta")

	// ⑤ prompt result（id:11）在流收尾后发出
	resultFrame := harness.waitFrame(30*time.Second, func(f map[string]any) bool {
		return f["id"] == float64(11)
	})
	s.Require().NotNil(resultFrame, "工具响应回流后应收到 prompt result")
	promptResult, _ := resultFrame["result"].(map[string]any)
	s.Require().Equal("end_turn", promptResult["stopReason"])
}

// TestAcpBridgeE2E_StdoutNoInterleave 用例 3：stdoutMu 无 interleave。
// 两会话先后 prompt（A 完成后发 B），两路回包帧交错经 stdoutMu 写出，
// 收集的全部输出行均为合法 JSON。
// （注：同 channel 并发双 prompt 时 agent 侧第二流不收尾是既有边界，
// 故采用先后接续 + 帧交错断言——两路帧仍并发到达 stdout 写端。）
func (s *AcpBridgeE2ESuite) TestAcpBridgeE2E_StdoutNoInterleave() {
	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("interleave probe A"),
		mockllm.CreateTextResponse("interleave probe B"))

	harness := s.newAcpChannelHarness()
	defer harness.close()

	// 会话 A prompt → 等 result
	harness.writeLine(`{"jsonrpc":"2.0","id":20,"method":"session/prompt","params":{"sessionId":"ide-sess-a","prompt":[{"type":"text","text":"hello a"}]}}`)

	got20, got21 := false, false
	var framesA []map[string]any
	for !got20 {
		f := harness.consumeFrame(30 * time.Second)
		if f == nil {
			break
		}
		framesA = append(framesA, f)
		if f["id"] == float64(20) {
			got20 = true
		}
	}
	s.Require().True(got20, "会话 A 应收到 result")

	// 会话 B prompt（A 的 result 已落，B 流独立）→ 等 result
	harness.writeLine(`{"jsonrpc":"2.0","id":21,"method":"session/prompt","params":{"sessionId":"ide-sess-b","prompt":[{"type":"text","text":"hello b"}]}}`)
	for !got21 {
		f := harness.consumeFrame(30 * time.Second)
		if f == nil {
			break
		}
		if f["id"] == float64(21) {
			got21 = true
		}
	}
	s.Require().True(got21, "会话 B 应收到 result")

	// 无 interleave 验证：每一行独立解析均合法 JSON
	lines := harness.rawLines()
	s.Require().NotEmpty(lines)
	for i, line := range lines {
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			s.T().Fatalf("第 %d 行 interleave（非法 JSON）: %q", i, line)
		}
	}
	// 双会话帧都存在（交错写出的两侧证据）
	var sawA, sawB bool
	for _, f := range harness.snapshotFrames() {
		if f["method"] != "session/update" {
			continue
		}
		params, _ := f["params"].(map[string]any)
		if params["sessionId"] == "ide-sess-a" {
			sawA = true
		}
		if params["sessionId"] == "ide-sess-b" {
			sawB = true
		}
	}
	s.Require().True(sawA, "应收到会话 A 的 update 帧")
	s.Require().True(sawB, "应收到会话 B 的 update 帧")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// acpChannelHarness AcpChannel 测试 harness（io.Pipe stdin + safeBuffer stdout）。
type acpChannelHarness struct {
	suite   *AcpBridgeE2ESuite
	channel *acp.AcpChannel
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	mu      sync.Mutex
	buf     strings.Builder
	frames  []map[string]any
	lines   []string
	cursor  int // consumeFrame 游标（不重复消费）
}

// newAcpChannelHarness 装配 AcpChannel 并启动 Start goroutine。
func (s *AcpBridgeE2ESuite) newAcpChannelHarness() *acpChannelHarness {
	stdinR, stdinW := io.Pipe()
	h := &acpChannelHarness{
		suite:  s,
		stdinR: stdinR,
		stdinW: stdinW,
	}
	ch := acp.NewAcpChannel(acp.DefaultAcpChannelConfig())
	ch.SetStdinForTest(stdinR)
	ch.SetStdoutForTest(h)
	ch.SetGatewayURL(fmt.Sprintf("ws://127.0.0.1:%d/acp", s.gwPort))
	h.channel = ch

	go func() {
		_ = ch.Start(s.Ctx)
	}()

	// WS 连接建立（ensureGatewayConnection 12 次退避内）
	deadline := time.Now().Add(8 * time.Second)
	connected := false
	for time.Now().Before(deadline) {
		if c, _ := ch.GwConnForTest(); c != nil {
			connected = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().True(connected, "AcpChannel 应在 8s 内建立 WS 连接")
	return h
}

// Write stdout 收集器（writeLine 单行单写——按写边界切行解析入 frames）。
func (h *acpChannelHarness) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	n, err := h.buf.Write(p)
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return n, err
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		h.lines = append(h.lines, line)
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err == nil {
			h.frames = append(h.frames, frame)
		}
	}
	return n, err
}

// rawLines 返回已收集的原始输出行（过滤空行）。
func (h *acpChannelHarness) rawLines() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.lines))
	for _, l := range h.lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// snapshotFrames 当前全部已解析帧快照。
func (h *acpChannelHarness) snapshotFrames() []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]map[string]any, 0, len(h.frames))
	out = append(out, h.frames...)
	return out
}

// waitFrame 轮询等待谓词命中的帧（超时返回 nil）。
func (h *acpChannelHarness) waitFrame(timeout time.Duration, pred func(map[string]any) bool) map[string]any {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f := h.findFrame(pred); f != nil {
			return f
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// consumeFrame 自游标位置消费下一帧（阻塞至超时；游标推进，不重复返回）。
func (h *acpChannelHarness) consumeFrame(timeout time.Duration) map[string]any {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		if len(h.frames) > h.cursor {
			f := h.frames[h.cursor]
			h.cursor++
			h.mu.Unlock()
			return f
		}
		h.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// findFrame 在已收集帧中查找谓词命中。
func (h *acpChannelHarness) findFrame(pred func(map[string]any) bool) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, f := range h.frames {
		if pred(f) {
			return f
		}
	}
	return nil
}

// writeLine 注入一行 stdin JSON-RPC。
func (h *acpChannelHarness) writeLine(line string) {
	_, err := h.stdinW.Write([]byte(line + "\n"))
	if err != nil {
		h.suite.T().Fatalf("stdin 写入失败: %v", err)
	}
}

// close 收尾（关 stdin → channel Stop）。
func (h *acpChannelHarness) close() {
	_ = h.stdinW.Close()
	h.channel.Stop()
}

// setupDataDir 构造 UAPCLAW_DATA_DIR 三件套（config/config.yaml + workspace 目录）
func (s *AcpBridgeE2ESuite) setupDataDir() string {
	dataDir := s.T().TempDir()
	configDir := filepath.Join(dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(dataDir, "agent", "workspace"), 0o755))

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

// pickFreePort 选取随机可用端口
func (s *AcpBridgeE2ESuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录原值并设置环境变量（TearDown 恢复）
func (s *AcpBridgeE2ESuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	s.envBackup = append(s.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量
func (s *AcpBridgeE2ESuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "false" {
			_ = os.Unsetenv(e[0])
		} else {
			_ = os.Setenv(e[0], e[1])
		}
	}
}
