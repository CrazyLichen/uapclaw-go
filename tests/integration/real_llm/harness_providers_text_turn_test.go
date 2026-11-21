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

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/external"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
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

// TestRealLLM_codebuddyTeamMcp 真 CLI 拉起真 team MCP server 的端到端验证：
// 入团描述符（OPENJIWEN_TEAM_JOIN env，sqlite 落盘库 + inprocess transport）
// → harness 翻译成 --mcp-config → 真 codebuddy 子进程拉起真 uapclawmcpserver
// → server 急切 Connect（库 schema 落盘）。断言打确定性事实：轮次协议不变量
// + 团队库文件被 server 真实创建（连接成功的产物），不断言模型行为。
func (s *HarnessProviderTextTurnSuite) TestRealLLM_codebuddyTeamMcp() {
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
	dir := s.T().TempDir()
	serverBin := filepath.Join(dir, "uapclawmcpserver")
	// 从仓库根构建真 team MCP server 二进制
	workRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	s.Require().NoError(err)
	build := exec.Command("go", "build", "-o", serverBin, "./cmd/uapclawmcpserver")
	build.Dir = workRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		s.Require().NoError(buildErr, "构建 uapclawmcpserver 失败: %s", output)
	}

	// 入团描述符：sqlite 落盘库（连接成功的可观测产物）+ inprocess transport
	dbPath := filepath.Join(dir, "team.db")
	descriptor := external.TeamJoinDescriptor{
		SessionID:  "e2e-mcp-session",
		TeamName:   "e2e-mcp-team",
		MemberName: "worker",
		Role:       "teammate",
		Scope:      "member",
		DBConfig: database.DatabaseConfig{DBType: database.DatabaseTypeSQLite,
			ConnectionString: dbPath, DBTimeout: 30, DBEnableWAL: true},
		TransportConfig: &schema.MessagerTransportConfig{Backend: "inprocess"},
	}
	harness, err := codebuddy.NewCodebuddyHarnessWithFactory(codebuddy.CodebuddyConfig{
		CliPath:            cliPath,
		Model:              model,
		TurnTimeoutSeconds: 180,
		Env:                descriptor.ToEnv(),
	}, func(options codebuddy.TransportOptions) (codebuddy.Transport, error) {
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
			Command: []string{serverBin},
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
	// 确定性事实：真 team server 被拉起且 Connect 成功（库 schema 落盘）
	info, statErr := os.Stat(dbPath)
	s.Require().NoError(statErr, "团队库未创建——team MCP server 未被 CLI 拉起或连接失败")
	s.Require().Greater(info.Size(), int64(0), "团队库为空——server 未完成 Initialize")
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
