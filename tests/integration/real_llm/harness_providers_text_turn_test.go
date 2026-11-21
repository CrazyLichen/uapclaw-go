//go:build integration && llm

package real_llm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/codebuddy"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/dsh"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessProviderTextTurnSuite 15.17 真 LLM 最小份额：run_text_turn +
// assert_turn_invariants 两条契约（对齐 _contract.py 的非 mock 份额；
// 其余 6 条契约 ⤵️ real_llm 待补，见 spec D-15.17-a）。
//
// 双路径：
//  1. dsh provider（DSH_HOME 指向真实 dsh 运行时，缺失时 Skip）
//  2. codebuddy provider（CLI 2.181.0+，缺失时 Skip；模型默认 hy4flash-free）
//
// 断言只打确定性契约（invariants + FINISHED + answer 非空），不断言
// 模型行为路径（对齐 real-llm 断言范围守卫记忆）。
type HarnessProviderTextTurnSuite struct {
	suite.Suite
	// fakeBinHolder 占位（codebuddy 直用真 CLI）
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHarnessProviderTextTurnSuite 套件入口。
func TestHarnessProviderTextTurnSuite(t *testing.T) {
	suite.Run(t, new(HarnessProviderTextTurnSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestRealLLM_dshTextTurn dsh provider 真跑一轮 text turn。
func (s *HarnessProviderTextTurnSuite) TestRealLLM_dshTextTurn() {
	if os.Getenv("DSH_HOME") == "" {
		s.T().Skip("DSH_HOME 未配置，跳过 dsh 真跑")
	}
	harness, err := dsh.NewDshHarness(dsh.DshHarnessConfig{DshHome: os.Getenv("DSH_HOME")})
	s.Require().NoError(err)
	s.runTextTurn(harness, "dsh")
}

// TestRealLLM_codebuddyTextTurn codebuddy provider 真跑一轮 text turn。
func (s *HarnessProviderTextTurnSuite) TestRealLLM_codebuddyTextTurn() {
	cliPath := os.Getenv("CODEBUDDY_CLI_PATH")
	if cliPath == "" {
		cliPath = "codebuddy"
	}
	if _, err := exec.LookPath(cliPath); err != nil {
		s.T().Skip("codebuddy CLI 不在 PATH，跳过真跑")
	}
	model := os.Getenv("CODEBUDDY_MODEL")
	if model == "" {
		model = "hy4flash-free"
	}
	harness, err := codebuddy.NewCodebuddyHarness(codebuddy.CodebuddyConfig{
		CliPath:            cliPath,
		Model:              model,
		TurnTimeoutSeconds: 180,
	})
	s.Require().NoError(err)
	s.runTextTurn(harness, "codebuddy")
}

// TestRealLLM_codebuddyMcpLoad 真 CLI 加载宿主 MCP server 的端到端验证：
// context.McpServers → harness 翻译成 --mcp-config → 真 codebuddy 子进程
// 真实拉起 MCP server（python3 极简 JSON-RPC server 落盘 initialize 日志）。
// 断言打确定性事实（日志文件出现 initialize 请求），不断言模型行为。
func (s *HarnessProviderTextTurnSuite) TestRealLLM_codebuddyMcpLoad() {
	cliPath := os.Getenv("CODEBUDDY_CLI_PATH")
	if cliPath == "" {
		cliPath = "codebuddy"
	}
	if _, err := exec.LookPath(cliPath); err != nil {
		s.T().Skip("codebuddy CLI 不在 PATH，跳过真跑")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		s.T().Skip("python3 不在 PATH，跳过 MCP 加载验证")
	}
	model := os.Getenv("CODEBUDDY_MODEL")
	if model == "" {
		model = "hy4flash-free"
	}
	dir := s.T().TempDir()
	logFile := filepath.Join(dir, "mcp-server.log")
	serverScript := filepath.Join(dir, "fake_mcp_server.py")
	// 极简 stdio MCP server：收到 JSON-RPC initialize 即落盘日志并应答
	script := `import json,sys
log = open('` + logFile + `','a')
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    if req.get("method") == "initialize":
        log.write("initialize\n")
        log.flush()
        resp = {"jsonrpc":"2.0","id":req.get("id"),"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fake-team","version":"0.0.1"}}}
        print(json.dumps(resp), flush=True)
    elif req.get("method") == "notifications/initialized":
        pass
`
	s.Require().NoError(os.WriteFile(serverScript, []byte(script), 0o600))
	harness, err := codebuddy.NewCodebuddyHarnessWithFactory(codebuddy.CodebuddyConfig{
		CliPath:            cliPath,
		Model:              model,
		TurnTimeoutSeconds: 180,
	}, func(options codebuddy.TransportOptions) (codebuddy.Transport, error) {
		s.T().Logf("transport argv: %s", strings.Join(options.Args, " "))
		return codebuddy.NewProcessTransport(options)
	})
	s.Require().NoError(err)
	ctx := context.Background()
	hctx := harnessprotocol.HarnessContext{
		AgentName:       "e2e-codebuddy-mcp",
		AgentID:         "e2e_codebuddy_mcp",
		HostSessionID:   "e2e-host-session-mcp",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
		McpServers: []harnessprotocol.McpServerConfig{{
			Name: "openjiuwen-team", Transport: harnessprotocol.McpTransportStdio,
			Command: []string{"python3", serverScript},
		}},
	}
	s.Require().NoError(harness.Start(ctx, hctx))
	receipt, err := harness.Send(ctx,
		harnessprotocol.HarnessInput{Content: "Reply with exactly the single word PONG and nothing else."},
		harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectEvents(harness.TurnEvents(&receipt.TurnID))
	testhelpers.AssertTurnInvariants(s.T(), events, receipt.TurnID)
	terminal, _ := harnessTerminalAndAnswer(events)
	s.Require().Equal(harnessprotocol.TurnEventKindFinished, terminal, "MCP 验证轮未正常完成")
	s.Require().NoError(harness.Stop(ctx))
	// 确定性事实：真 CLI 真的拉起了 MCP server（initialize 握手落盘）
	logged, readErr := os.ReadFile(logFile)
	s.Require().NoError(readErr, "MCP server 日志未产生——CLI 未加载 --mcp-config")
	s.Require().Contains(string(logged), "initialize", "MCP server 未收到 initialize 握手")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runTextTurn run_text_turn 语义：Send(AUTO) → 消费轮事件 → invariants +
// FINISHED + answer 含 PONG → Stop 后 TERMINATED（对齐 _contract.py:124-141）。
func (s *HarnessProviderTextTurnSuite) runTextTurn(
	harness harnessprotocol.HarnessProtocol, label string) {
	ctx := context.Background()
	s.Require().NoError(harness.Start(ctx, harnessprotocol.HarnessContext{
		AgentName:       "e2e-" + label,
		AgentID:         "e2e_" + label,
		HostSessionID:   "e2e-host-session-" + label,
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	receipt, err := harness.Send(ctx,
		harnessprotocol.HarnessInput{Content: "Reply with exactly the single word PONG and nothing else."},
		harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectEvents(harness.TurnEvents(&receipt.TurnID))
	testhelpers.AssertTurnInvariants(s.T(), events, receipt.TurnID)
	terminal, answer := harnessTerminalAndAnswer(events)
	s.Require().Equal(harnessprotocol.TurnEventKindFinished, terminal, label+" 轮未正常完成")
	s.Require().NotEmpty(strings.TrimSpace(answer), label+" answer 为空")
	s.Require().NoError(harness.Stop(ctx))
	s.Require().Equal(harnessprotocol.HarnessStateTerminated, harness.State())
}

// collectEvents 消费游标全部事件。
func (s *HarnessProviderTextTurnSuite) collectEvents(
	cursor harnessprotocol.HarnessEventCursor) []harnessprotocol.HarnessEvent {
	defer func() { _ = cursor.Close(context.Background()) }()
	var events []harnessprotocol.HarnessEvent
	for {
		event, err := cursor.Next(context.Background())
		if err != nil {
			break
		}
		events = append(events, *event)
	}
	return events
}

// harnessTerminalAndAnswer 提取终态与 answer 文本。
func harnessTerminalAndAnswer(events []harnessprotocol.HarnessEvent) (
	harnessprotocol.TurnEventKind, string) {
	kind := harnessprotocol.TurnEventKind("")
	answer := strings.Builder{}
	for _, event := range events {
		switch payload := event.Event.(type) {
		case harnessprotocol.TurnLifecycleEvent:
			if payload.Kind != harnessprotocol.TurnEventKindStarted {
				kind = payload.Kind
			}
		case harnessprotocol.OutputEvent:
			if text, ok := payload.Content.(string); ok {
				answer.WriteString(text)
			}
		}
	}
	return kind, answer.String()
}
