//go:build integration

package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// slowStreamClient 每 delay 吐一个 chunk 的假模型，用于制造"旧轮仍在输出"的窗口。
//
// 必须实现完整 BaseModelClient 接口才能注册进 ClientRegistry。
// 第 1 次 Stream 走慢速三段（便于打断），第 2 次起立即返回单段文本，
// 这样用例能区分"第一轮的收尾"与"第二轮真的跑完了"。
type slowStreamClient struct {
	// chunks 首次 Stream 依次输出的文本片段
	chunks []string
	// delay 每个片段之间的间隔
	delay time.Duration
	// calls Stream 调用计数（原子读写）
	calls int32
}

// SupplementInterruptSuite 验证 supplement 打断后新一轮不被打死、且不产生 chat.error。
type SupplementInterruptSuite struct {
	isuite.BaseIntegrationSuite
	// providerName 注册到 ClientRegistry 的 mock provider 名
	providerName string
	// dataDir 隔离的数据目录
	dataDir string
	// origWd 套件开始前的工作目录
	origWd string
	// wsPort Web 通道监听端口
	wsPort int
	// gatewayPort 多路由 WS 服务器端口（默认 19001，必须隔离，否则并行包互撞）
	gatewayPort int
	// agentServer 进程内 AgentServer
	agentServer *server.AgentServer
	// gwServer 进程内 AppGatewayServer
	gwServer *gateway.AppGatewayServer
	// envBackup 环境变量备份（[key, 原值, 是否原本存在]）
	envBackup [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSupplementInterruptSuite 套件入口。
func TestSupplementInterruptSuite(t *testing.T) {
	suite.Run(t, new(SupplementInterruptSuite))
}

// TestSupplement_SlowRoundInterrupted_NewRoundCompletes 是本用例的核心断言：
// ① 打断后新一轮必须拿到非空 chat.final；② 全程不得出现 chat.error（D2）；
// ③ 历史里不得落 chat.error 记录（D2 的 history 侧）。
//
// D1 的确定性由单测 TestReleaseStreamTask_OnlyRemovesOwnEntry 保证；
// 本用例验证端到端契约，慢速流把"旧轮仍在收尾"的窗口拉长以提高命中率。
func (s *SupplementInterruptSuite) TestSupplement_SlowRoundInterrupted_NewRoundCompletes() {
	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	defer func() { _ = conn.Close() }()

	sessionID := fmt.Sprintf("sess_supp_%d", time.Now().UnixNano())
	s.Require().NoError(s.sendChatSend(conn, "req-supp-1", sessionID, "第一轮"))

	// 等第一轮开始出字，再打断（慢速流每 120ms 一段，200ms 足够进入流式阶段）
	time.Sleep(200 * time.Millisecond)
	s.Require().NoError(s.sendSupplement(conn, "req-supp-int", sessionID, "改成第二件事"))

	var (
		sawError     string
		sawInterrupt bool
		sawNewRound  bool
	)

	deadline := time.Now().Add(60 * time.Second)
	idle := 0
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			idle++
			if idle >= 6 {
				break
			}
			continue
		}
		idle = 0

		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		eventName, payload := suppFrameEvent(frame)
		switch eventName {
		case "chat.error":
			sawError = fmt.Sprintf("%v", payload["error"])
			s.T().Logf("收到 chat.error: %s", sawError)
		case "chat.interrupt_result":
			if intent, _ := payload["intent"].(string); intent == "supplement" {
				sawInterrupt = true
				s.T().Logf("收到 interrupt_result: intent=supplement")
			}
		case "chat.final":
			if content, _ := payload["content"].(string); content == "第二轮完成" {
				sawNewRound = true
				s.T().Logf("收到新轮 final: %s", content)
			}
		}
		if sawNewRound {
			break
		}
	}

	s.True(sawInterrupt, "应收到 supplement 的 interrupt_result（打断生效）")
	s.Empty(sawError, "打断后不得出现 chat.error，实际: %s", sawError)
	s.True(sawNewRound, "打断后新一轮应跑完并产出「第二轮完成」")

	// 防 D2 回归：取消不得写入 chat.error 历史记录
	session.FlushHistoryQueue()
	records, recErr := session.ReadHistoryRecords(sessionID)
	s.Require().NoError(recErr)
	for _, r := range records {
		s.NotEqual("chat.error", r["event_type"], "取消不得写入 chat.error 记录: %+v", r)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Invoke 非流式调用：拼接全部 chunk。
func (c *slowStreamClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	joined := ""
	for _, seg := range c.chunks {
		joined += seg
	}
	return llmschema.NewAssistantMessage(joined), nil
}

// Stream 流式调用：首次逐段延迟输出（便于被打断），第二次起立即返回单段"第二轮完成"。
func (c *slowStreamClient) Stream(ctx context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	n := atomic.AddInt32(&c.calls, 1)
	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	if n > 1 {
		// 第二轮（含 supplement 之后的新一轮）：立即完成，便于用例判定"新轮真的跑完了"
		ch <- llmschema.NewAssistantMessageChunk("第二轮完成")
		close(ch)
		return ch, nil
	}
	go func() {
		defer close(ch)
		for _, seg := range c.chunks {
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.delay):
			}
			select {
			case <-ctx.Done():
				return
			case ch <- llmschema.NewAssistantMessageChunk(seg):
			}
		}
	}()
	return ch, nil
}

// GenerateImage 不支持图片生成。
func (c *slowStreamClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("slowStreamClient: GenerateImage unsupported")
}

// GenerateSpeech 不支持语音生成。
func (c *slowStreamClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("slowStreamClient: GenerateSpeech unsupported")
}

// GenerateVideo 不支持视频生成。
func (c *slowStreamClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("slowStreamClient: GenerateVideo unsupported")
}

// TranscribeAudio 不支持转写。
func (c *slowStreamClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("slowStreamClient: TranscribeAudio unsupported")
}

// Release 释放模型资源（no-op）。
func (c *slowStreamClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return true, nil
}

// SupportsKVCacheRelease 不支持 KV Cache 释放。
func (c *slowStreamClient) SupportsKVCacheRelease() bool { return false }

// Factory 返回注册到 ClientRegistry 的工厂。
func (c *slowStreamClient) Factory() model_clients.ClientFactory {
	return func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return c, nil
	}
}

// SetupSuite 装配进程内 serve（gateway + server + transport）。
//
// 装配步骤与 team_continuous_dialog_test.go:65-140 一致：
// ① 注册 mock provider ② 临时目录 + 切 CWD + 写 config/config.yaml
// ③ UAPCLAW_RESOURCES_DIR / DATA_DIR / WEB_PORT ④ workspace.Init
// ⑤ ChannelTransport 串联 AgentServer + AgentClient + AppGatewayServer。
// 差异：mock provider 换成 slowStreamClient（慢速流），config 不写 modes.team。
func (s *SupplementInterruptSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	s.providerName = fmt.Sprintf("slow_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", (&slowStreamClient{
		chunks: []string{"第一段", "第二段", "第三段"},
		delay:  120 * time.Millisecond,
	}).Factory())

	s.dataDir = s.T().TempDir()
	wd, err := os.Getwd()
	s.Require().NoError(err)
	s.origWd = wd
	s.Require().NoError(os.Chdir(s.dataDir))

	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))

	yaml := fmt.Sprintf(`workspace:
  path: %s
models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true
`, s.dataDir, s.providerName, s.providerName)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0o644))

	repoRoot, err := filepath.Abs(filepath.Join(s.origWd, "..", "..", "..", ".."))
	s.Require().NoError(err)
	s.saveAndSetenv("UAPCLAW_RESOURCES_DIR", filepath.Join(repoRoot, "resources"))

	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	pathutil.ResetCache()

	_, err = workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)

	// 三子系统构造（integration-audit 调用跨度来源，勿移到 helper 包）
	tr := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(tr)

	// 端口绑定重试：pickFreePort 是"listen :0 → 取号 → 关闭"，
	// 到网关真正 bind 之间有空窗，CI 并行时可能被别的进程抢占。
	// Start 非阻塞且端口被占时仍返回 nil，故必须探活 + 读 ListenErr 才能判定。
	for attempt := 1; attempt <= 3; attempt++ {
		s.wsPort = s.pickFreePort()
		s.gatewayPort = s.pickFreePort()
		if attempt == 1 {
			s.saveAndSetenv("UAPCLAW_WEB_PORT", fmt.Sprintf("%d", s.wsPort))
			s.saveAndSetenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", s.gatewayPort))
		} else {
			_ = os.Setenv("UAPCLAW_WEB_PORT", fmt.Sprintf("%d", s.wsPort))
			_ = os.Setenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", s.gatewayPort))
		}
		pathutil.ResetCache()

		cfg, cfgErr := config.New("")
		s.Require().NoError(cfgErr)
		_, loadErr := cfg.Load()
		s.Require().NoError(loadErr)

		if attempt == 1 {
			s.agentServer = server.NewAgentServer(cfg, tr)
			s.Require().NoError(s.agentServer.Start(s.Ctx))
		}
		gwServer, gwErr := gateway.NewAppGatewayServer(cfg, agentClient)
		s.Require().NoError(gwErr)
		s.Require().NoError(gwServer.Start(s.Ctx))

		if readyErr := waitTCPReady(gwServer, s.wsPort, 3*time.Second); readyErr == nil {
			s.gwServer = gwServer
			break
		} else {
			s.T().Logf("第 %d 次绑定端口 %d 失败（疑似并发抢占）：%v，换号重试", attempt, s.wsPort, readyErr)
			_ = gwServer.Stop()
			if attempt == 3 {
				s.FailNow("连续 3 次无法绑定 Web WS 端口")
			}
		}
	}
}

// TearDownSuite 逆序停机 + 清理注册与环境。
func (s *SupplementInterruptSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	s.restoreEnv()
	pathutil.ResetCache()
	if s.origWd != "" {
		_ = os.Chdir(s.origWd)
	}
	s.BaseIntegrationSuite.TearDownSuite()
}

// sendChatSend 发送 chat.send 请求帧。
func (s *SupplementInterruptSuite) sendChatSend(conn *websocket.Conn, id, sessionID, content string) error {
	req := map[string]any{
		"type":   "req",
		"id":     id,
		"method": "chat.send",
		"params": map[string]any{
			"session_id": sessionID,
			"mode":       "agent",
			"content":    content,
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

// sendSupplement 发送 supplement 中断帧（chat.interrupt + new_input）。
func (s *SupplementInterruptSuite) sendSupplement(conn *websocket.Conn, id, sessionID, newInput string) error {
	req := map[string]any{
		"type":   "req",
		"id":     id,
		"method": "chat.interrupt",
		"params": map[string]any{
			"session_id": sessionID,
			"mode":       "agent",
			"intent":     "supplement",
			"new_input":  newInput,
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

// pickFreePort 取一个空闲 TCP 端口。
func (s *SupplementInterruptSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录并设置环境变量。
func (s *SupplementInterruptSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量。
func (s *SupplementInterruptSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "1" {
			_ = os.Setenv(e[0], e[1])
		} else {
			_ = os.Unsetenv(e[0])
		}
	}
	s.envBackup = nil
}

// suppFrameEvent 从 wire 帧中取事件名与 payload。
//
// 事件名既可能出现在顶层 event（gateway 归一化后），也可能在 payload.event_type。
func suppFrameEvent(frame map[string]any) (string, map[string]any) {
	payload, _ := frame["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	if name, ok := frame["event"].(string); ok && name != "" {
		return name, payload
	}
	name, _ := payload["event_type"].(string)
	return name, payload
}

// waitTCPReady 等待指定端口真正可连接，失败时附带网关监听错误作诊断。
//
// Start 是非阻塞的（ListenAndServe 在 goroutine 内），端口被并发抢占时
// Start 仍返回 nil，必须靠探活判定；ListenErr 只用于把真实原因带出来。
func waitTCPReady(gw *gateway.AppGatewayServer, port int, timeout time.Duration) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	var listenErr <-chan error
	if gw != nil {
		listenErr = gw.ListenErr()
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if listenErr != nil {
		select {
		case err := <-listenErr:
			if err != nil {
				return fmt.Errorf("端口 %d 未就绪；网关监听错误: %w", port, err)
			}
		default:
		}
	}
	return fmt.Errorf("端口 %d 在 %s 内未就绪", port, timeout)
}

// SupportsKVCacheAffinity 测试替身：不支持 KV 亲和。
func (c *slowStreamClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 测试替身：返回空亲和参数。
func (c *slowStreamClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}
