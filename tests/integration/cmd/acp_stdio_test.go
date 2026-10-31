//go:build integration

package cmd_test

// acp_stdio_test.go 入站 ACP stdio 黑盒集成测试（ACP stdio 迁移 Task 10-14）。
//
// 链路：测试驱动 → 真 uapclaw acp 子进程（AcpChannel stdio）→ 真 WS →
// 父进程 GatewayServer /acp + AcpGatewayBridge → InboundGatewayServer →
// MessageHandler → E2A → AgentServer（MockLLM）→ session/update/result 帧 → 子进程 stdout。
//
// 选 mode-2（--gateway-url 指回父进程）而非子进程自举：子进程只跑 AcpChannel，
// LLM 仍由父进程 MockLLM 打桩，避免在子进程内伪造 OpenAI 服务与完整 agents 配置。
//
// 对应 Python：jiuwenswarm/channels/acp/app_acp.py run_acp +
// tests/system_tests/test_acp_cli_stdio.py（Python 侧用 Popen 打桩，本用例为真子进程）。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpStdioBinarySuite 入站黑盒套件：父进程托管网关，子进程跑真 uapclaw acp
type AcpStdioBinarySuite struct {
	isuite.BaseIntegrationSuite
	// inboundServer 入站网关
	inboundServer *gateway.InboundGatewayServer
	// gwServer 网关服务（/acp 路由）
	gwServer *gateway.GatewayServer
	// channelMgr 渠道管理器
	channelMgr *channel_manager.ChannelManager
	// msgHandler 消息处理器
	msgHandler *mh.MessageHandler
	// agentClient Agent 客户端
	agentClient *routing.AgentClient
	// chTransport 进程内 transport
	chTransport *transport.ChannelTransport
	// agentServer Agent 服务
	agentServer *server.AgentServer
	// gwPort 网关随机端口
	gwPort int
	// providerName mock provider 注册名
	providerName string
	// uapclawBin 构建出的 CLI 二进制路径
	uapclawBin string
	// envBackup 环境变量备份（[key, 原值, 是否存在]）
	envBackup [][3]string
}

// childProc 子进程句柄与帧读取器
type childProc struct {
	// cmd 子进程
	cmd *exec.Cmd
	// stdin 行写缓冲
	stdin *bufio.Writer
	// stdinW 显式持有的 stdin 写端（EOF 用例关闭用）
	stdinW io.WriteCloser
	// frames 已解析的 JSON-RPC 帧
	frames chan map[string]any
	// stderr 子进程 stderr（诊断 + 非 JSON stdout 行）
	stderr *strings.Builder
	// dirty 非 JSON 的 stdout 行计数（协议帧污染检测）
	dirty int32
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAcpStdioBinarySuite 运行入站黑盒套件
func TestAcpStdioBinarySuite(t *testing.T) {
	suite.Run(t, new(AcpStdioBinarySuite))
}

// SetupSuite 装配 7 件套 + 构建 uapclaw 二进制
func (s *AcpStdioBinarySuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	s.providerName = fmt.Sprintf("mock_acp_stdio_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.setupDataDir())
	pathutil.ResetCache()

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

	s.gwPort = pickFreePort(s.T())
	gwCfg := gateway.NewGatewayServerConfig()
	gwCfg.Host = "127.0.0.1"
	gwCfg.Port = s.gwPort
	gwCfg.Routes = gateway.BuildRouteConfigMap([]*routing.RouteBinding{
		gateway.BuildACPRouteBinding("/acp", "acp"),
	})
	s.gwServer = gateway.NewGatewayServer(gwCfg)

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
	s.channelMgr.RegisterExternalChannel("acp", s.gwServer)
	s.gwServer.OnMessage(s.inboundServer.HandleMessage)

	s.Require().NoError(s.agentServer.Start(s.Ctx))
	s.Require().NoError(s.agentClient.Connect(s.Ctx))
	s.Require().NoError(s.msgHandler.StartForwarding(s.Ctx))
	s.Require().NoError(s.channelMgr.StartDispatch(s.Ctx))
	s.Require().NoError(s.inboundServer.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))

	bin := filepath.Join(s.T().TempDir(), "uapclaw")
	root := moduleRoot(s.T())
	build := exec.Command("go", "build", "-o", bin, "./cmd/uapclaw/")
	build.Dir = root
	if out, berr := build.CombinedOutput(); berr != nil {
		s.T().Skipf("构建 uapclaw 失败，跳过: %v\n%s", berr, out)
	}
	s.uapclawBin = bin
}

// TearDownSuite 逆序停止 + 清理
func (s *AcpStdioBinarySuite) TearDownSuite() {
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

// TestAcpStdioBinary_探测首帧 打印子进程首帧，确认协议通道与桩日志分离
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_探测首帧() {
	child := s.startChild("")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	frame := child.waitFrame(15*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) })
	s.Require().NotNil(frame, "initialize 应有响应；stderr=%s", child.stderr.String())
	s.T().Logf("首帧: %v", frame)
	s.NotNil(frame["result"])
}

// TestAcpStdioBinary_PromptRoundTrip initialize→session/new→session/prompt 全链路往返
// 对应 Python test_acp_cli_returns_final_result（忽略 chunk、取 final result）
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_PromptRoundTrip() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("acp stdio binary reply"))

	child := s.startChild("")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	init := child.waitFrame(15*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) })
	s.Require().NotNil(init, "initialize 应有响应；stderr=%s", child.stderr.String())

	child.writeLine(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"sessionId":"ide-stdio-1"}}`)
	newFrame := child.waitFrame(15*time.Second, func(f map[string]any) bool { return f["id"] == float64(2) })
	s.Require().NotNil(newFrame, "session/new 应有响应；stderr=%s", child.stderr.String())

	child.writeLine(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"ide-stdio-1","prompt":[{"type":"text","text":"hello binary"}]}}`)

	// 会话帧：session/update 且文本含 MockLLM 固定回复
	update := child.waitFrame(30*time.Second, func(f map[string]any) bool {
		if f["method"] != "session/update" {
			return false
		}
		return strings.Contains(fmt.Sprintf("%v", f), "acp stdio binary reply")
	})
	s.Require().NotNil(update, "应收到 agent_message_chunk；stderr=%s", child.stderr.String())

	// 终帧：result 且 stopReason=end_turn
	final := child.waitFrame(30*time.Second, func(f map[string]any) bool {
		if f["id"] != float64(3) {
			return false
		}
		result, _ := f["result"].(map[string]any)
		return result != nil && result["stopReason"] == "end_turn"
	})
	s.Require().NotNil(final, "应收到 end_turn 终帧；stderr=%s", child.stderr.String())
}

// TestAcpStdioBinary_错误终帧 上游失败时子进程输出 error 帧（-32603）
// 对应 Python test_acp_cli_returns_final_jsonrpc_error。
//
// 前置生产修复：task_loop 产出 *cschema.ControllerOutputPayload 结构体 payload，
// 消费端原先只断言 map[string]any，导致 task_failed 整块丢弃、只剩 idle → end_turn。
// 修复后 utils.PayloadToMap 桥接结构体，本用例断言真实的 error 帧。
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_错误终帧() {
	s.MockLLM.SetResponses(mockllm.CreateErrorResponse(fmt.Errorf("upstream boom")))

	child := s.startChild("")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	s.Require().NotNil(child.waitFrame(15*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) }),
		"initialize 应有响应；stderr=%s", child.stderr.String())

	child.writeLine(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"sessionId":"ide-err-1"}}`)
	s.Require().NotNil(child.waitFrame(15*time.Second, func(f map[string]any) bool { return f["id"] == float64(2) }),
		"session/new 应有响应；stderr=%s", child.stderr.String())

	child.writeLine(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"ide-err-1","prompt":[{"type":"text","text":"boom"}]}}`)

	terminal := child.waitFrame(30*time.Second, func(f map[string]any) bool {
		return f["id"] == float64(3) && (f["result"] != nil || f["error"] != nil)
	})
	s.Require().NotNil(terminal, "应有终帧；stderr=%s", child.stderr.String())

	errObj, ok := terminal["error"].(map[string]any)
	s.Require().True(ok, "终帧应为 error（对齐 Python），实际 %v", terminal)
	s.Equal(float64(-32603), errObj["code"], "error code 应为 -32603")
	msg, _ := errObj["message"].(string)
	s.NotEmpty(msg, "error 应带 message")
	// 上游错误文本应透传到 IDE（便于排查，如 API 认证失败）
	s.Contains(msg, "upstream boom")
}

// TestAcpStdioBinary_stdinEOF优雅退出 关 stdin 后子进程在宽限内退出码 0（Go 增强）
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_stdinEOF优雅退出() {
	child := s.startChild("")
	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	s.Require().NotNil(child.waitFrame(15*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) }),
		"initialize 应有响应；stderr=%s", child.stderr.String())

	// 关 stdin 写端（等价 IDE 断开）→ 子进程应在 5s 宽限内自然退出
	s.Require().NoError(child.stdin.Flush())
	s.Require().NoError(child.stdinW.Close())

	done := make(chan error, 1)
	go func() { done <- child.cmd.Wait() }()
	select {
	case err := <-done:
		s.NoError(err, "stdin EOF 后应正常退出；stderr=%s", child.stderr.String())
	case <-time.After(15 * time.Second):
		_ = child.cmd.Process.Kill()
		s.FailNow("标准输入 EOF 后 15s 未退出")
	}
}

// TestAcpStdioBinary_远端不可达 网关不可达时子进程非零退出（Go 增强）
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_远端不可达() {
	child := s.startChild("ws://127.0.0.1:1/acp")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	// 读帧直到通道关闭（子进程退出）
	for range child.frames {
	}

	done := make(chan error, 1)
	go func() { done <- child.cmd.Wait() }()
	select {
	case err := <-done:
		s.Error(err, "网关不可达应非零退出")
		s.NotEmpty(child.stderr.String(), "stderr 应有可诊断信息")
	case <-time.After(30 * time.Second):
		_ = child.cmd.Process.Kill()
		s.FailNow("网关不可达时子进程 30s 未退出")
	}
}

// TestAcpStdioBinary_stdout纯协议帧 子进程日志全部走 stderr，stdout 每行都是合法 JSON
// 验证 os.Stdout=os.Stderr 重定向 + acpStdout 早捕获（Go 增强）
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_stdout纯协议帧() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("protocol only"))

	child := s.startChild("")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	s.Require().NotNil(child.waitFrame(15*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) }),
		"initialize 应有响应；stderr=%s", child.stderr.String())
	child.writeLine(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"sessionId":"ide-dirty-1"}}`)
	s.Require().NotNil(child.waitFrame(15*time.Second, func(f map[string]any) bool { return f["id"] == float64(2) }),
		"session/new 应有响应；stderr=%s", child.stderr.String())
	child.writeLine(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"ide-dirty-1","prompt":[{"type":"text","text":"hi"}]}}`)
	s.Require().NotNil(child.waitFrame(30*time.Second, func(f map[string]any) bool {
		if f["id"] != float64(3) {
			return false
		}
		result, _ := f["result"].(map[string]any)
		return result != nil && result["stopReason"] == "end_turn"
	}), "应收到 end_turn 终帧；stderr=%s", child.stderr.String())

	s.Zero(atomic.LoadInt32(&child.dirty), "stdout 不应出现非 JSON 行；stderr=%s", child.stderr.String())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// startChild 启动 uapclaw acp 子进程（gatewayURL 为空表示用父进程端口）
func (s *AcpStdioBinarySuite) startChild(gatewayURL string) *childProc {
	if gatewayURL == "" {
		gatewayURL = fmt.Sprintf("ws://127.0.0.1:%d/acp", s.gwPort)
	}
	child := &childProc{stderr: &strings.Builder{}}
	child.cmd = exec.Command(s.uapclawBin, "acp", "--gateway-url", gatewayURL)
	// 子进程用独立 data dir（不与父进程共享 config，避免竞争）
	child.cmd.Env = append(os.Environ(), "UAPCLAW_DATA_DIR="+s.setupDataDir())
	child.cmd.Stderr = child.stderr

	stdin, err := child.cmd.StdinPipe()
	s.Require().NoError(err)
	stdout, err := child.cmd.StdoutPipe()
	s.Require().NoError(err)
	s.Require().NoError(child.cmd.Start())
	child.stdinW = stdin
	child.stdin = bufio.NewWriter(stdin)
	child.frames = make(chan map[string]any, 64)

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var frame map[string]any
			if uerr := json.Unmarshal([]byte(line), &frame); uerr != nil {
				child.stderr.WriteString("[non-json stdout] " + line + "\n")
				atomic.AddInt32(&child.dirty, 1)
				continue
			}
			child.frames <- frame
		}
		close(child.frames)
	}()
	return child
}

// writeLine 写一行 JSON-RPC
func (c *childProc) writeLine(line string) {
	_, _ = c.stdin.WriteString(line + "\n")
	_ = c.stdin.Flush()
}

// waitFrame 等一帧满足条件的输出（超时返回 nil）
func (c *childProc) waitFrame(timeout time.Duration, pred func(map[string]any) bool) map[string]any {
	deadline := time.After(timeout)
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				return nil
			}
			if pred(f) {
				return f
			}
		case <-deadline:
			return nil
		}
	}
}

// kill 关 stdin 并确保子进程回收
func (c *childProc) kill() {
	if c.stdin != nil {
		_ = c.stdin.Flush()
	}
	if c.cmd.Process == nil {
		return
	}
	_ = c.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = c.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = c.cmd.Process.Kill()
		<-done
	}
}

// setupDataDir 构造 UAPCLAW_DATA_DIR 三件套
func (s *AcpStdioBinarySuite) setupDataDir() string {
	dataDir := s.T().TempDir()
	configDir := filepath.Join(dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(dataDir, "agent", "workspace"), 0o755))
	yamlContent := fmt.Sprintf(`server:
  agentserver:
    host: "127.0.0.1"
    port: 0
  gateway:
    host: "127.0.0.1"
    port: %d
models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true
`, s.gwPort, s.providerName, s.providerName)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yamlContent), 0o644))
	return dataDir
}

// moduleRoot 返回模块根目录（测试进程 cwd 是包目录，构建 CLI 需回到根）
func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD 失败: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatalf("无法定位 go.mod: %q", gomod)
	}
	return filepath.Dir(gomod)
}

// pickFreePort 取随机可用端口
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听随机端口失败: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录原值并设置
func (s *AcpStdioBinarySuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	s.envBackup = append(s.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量
func (s *AcpStdioBinarySuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "false" {
			_ = os.Unsetenv(e[0])
		} else {
			_ = os.Setenv(e[0], e[1])
		}
	}
}
