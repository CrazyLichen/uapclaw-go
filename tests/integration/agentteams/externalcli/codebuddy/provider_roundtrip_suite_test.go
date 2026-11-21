//go:build integration

package codebuddy_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	baseschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	providers "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/codebuddy"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProviderRoundtripSuite codebuddy provider 真子进程 stdio 双工套件。
//
// 跨子系统：harness_providers/codebuddy（provider + transport 真子进程）→
// harness_protocol（事件信封与协议不变量）。Python 无对应物（偏差
// D-15.22-a）：Python 进程级契约走真 LLM 系统测试，本目录以假 CLI 进程
// 承担等价的进程边界验证（spec §4）。
type ProviderRoundtripSuite struct {
	suite.Suite
	// fakeBin 假 CLI 可执行路径
	fakeBin string
	// scriptsDir 帧脚本目录
	scriptsDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestProviderRoundtripSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestProviderRoundtripSuite(t *testing.T) {
	suite.Run(t, new(ProviderRoundtripSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// SetupSuite 构建假 CLI 可执行文件并准备脚本目录。
func (s *ProviderRoundtripSuite) SetupSuite() {
	dir := s.T().TempDir()
	fakeBin := filepath.Join(dir, "fakecodebuddy")
	build := exec.Command("go", "build", "-o", fakeBin,
		"./tests/integration/agentteams/externalcli/codebuddy/fakecodebuddy")
	build.Dir = workRoot()
	output, err := build.CombinedOutput()
	s.Require().NoError(err, "构建 fakecodebuddy 失败: %s", output)
	s.fakeBin = fakeBin
	s.scriptsDir = dir
}

// TestRealRoundTrip_text整轮 假 CLI 真子进程整轮：恰一 STARTED + OutputEvent + FINISHED。
func (s *ProviderRoundtripSuite) TestRealRoundTrip_text整轮() {
	s.writeScript("success.jsonl", []string{
		`{"type":"assistant","session_id":"fake-session-1","message":{"content":[{"type":"text","text":"PONG"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"PONG","duration_ms":100,"num_turns":2}`,
	})
	harness := s.newScriptedHarness("success.jsonl")
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), s.newContext()))
	// 会话标识协商推迟到 ExecuteTurn 首帧（CLI 2.181.0 实测），Start 时为占位

	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "Reply PONG"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectTurnEvents(harness, receipt.TurnID)
	s.assertTurnInvariants(events, receipt.TurnID)

	terminal, output := s.terminalAndAnswer(events)
	s.Equal(harnessprotocol.TurnEventKindFinished, terminal)
	s.Contains(output, "PONG")
}

// TestRealRoundTrip_toolUse帧 tool_use 块 → tool item STARTED + COMPLETED。
func (s *ProviderRoundtripSuite) TestRealRoundTrip_toolUse帧() {
	s.writeScript("tool.jsonl", []string{
		`{"type":"assistant","session_id":"fake-session-1","message":{"content":[{"type":"tool_use","id":"call1","name":"read_file"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","duration_ms":100}`,
	})
	harness := s.newScriptedHarness("tool.jsonl")
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), s.newContext()))
	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "read"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectTurnEvents(harness, receipt.TurnID)
	s.assertTurnInvariants(events, receipt.TurnID)

	toolItems := 0
	for _, event := range events {
		if payload, ok := event.Event.(harnessprotocol.ItemLifecycleEvent); ok && payload.ItemType == "tool" {
			toolItems++
		}
	}
	s.Equal(2, toolItems, "tool item 应有 STARTED + COMPLETED 两条")
}

// TestRealRoundTrip_mcpContext到进程 MCP 注入端到端：HarnessContext.McpServers
// → harness 翻译成 --mcp-config → 假 CLI 进程解析并回显 server 名（验证
// context→argv→进程全链路；member_runtime 侧合并由 mergeHostMcpServers
// 单测覆盖）。fakecodebuddy 需 --echo-mcp 开关。
func (s *ProviderRoundtripSuite) TestRealRoundTrip_mcpContext到进程() {
	s.writeScript("mcp.jsonl", []string{
		`{"type":"assistant","session_id":"fake-session-1","message":{"content":[{"type":"text","text":"PONG"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"PONG","duration_ms":100}`,
	})
	path := filepath.Join(s.scriptsDir, "mcp.jsonl")
	harness, err := codebuddy.NewCodebuddyHarnessWithFactory(
		codebuddy.CodebuddyConfig{CliPath: s.fakeBin, TurnTimeoutSeconds: 30},
		func(options codebuddy.TransportOptions) (codebuddy.Transport, error) {
			options.Args = append(options.Args, "--script", path, "--echo-mcp")
			return codebuddy.NewProcessTransport(options)
		})
	s.Require().NoError(err)
	defer func() { _ = harness.Stop(context.Background()) }()
	hctx := s.newContext()
	hctx.McpServers = []harnessprotocol.McpServerConfig{{
		Name: "openuapclaw-team", Transport: harnessprotocol.McpTransportStdio,
		Command: []string{"uapclawmcpserver"},
	}}
	s.Require().NoError(harness.Start(context.Background(), hctx))
	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectTurnEvents(harness, receipt.TurnID)
	s.assertTurnInvariants(events, receipt.TurnID)
	_, answer := s.terminalAndAnswer(events)
	s.Contains(answer, "openuapclaw-team", "假 CLI 应收到 --mcp-config 并回显 server 名")
}

// TestRealRoundTrip_清单工厂路径 清单 → CreateHarness → provider 全链路；
// 假 CLI 无脚本时读完输入即退出 → 进程退出路径归 FAILED（最小失败分类）。
func (s *ProviderRoundtripSuite) TestRealRoundTrip_清单工厂路径() {
	card := saschema.NewAgentCard(func(ac *saschema.AgentCard) {
		ac.BaseCard = *baseschema.NewBaseCard(baseschema.WithName("e2e-codebuddy"),
			baseschema.WithDescription("e2e"))
	})
	manifest := hschema.AgentTemplateSpec{AgentCard: *card}
	protocol, err := providers.CreateHarness(manifest, providers.CreateHarnessOptions{
		Provider: "codebuddy",
		Config:   map[string]any{"cli_path": s.fakeBin, "turn_timeout_seconds": 30.0},
	})
	s.Require().NoError(err)
	harness := protocol.(*codebuddy.CodebuddyHarness)
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), s.newContext()))
	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectTurnEvents(harness, receipt.TurnID)
	s.assertTurnInvariants(events, receipt.TurnID)
	terminal, _ := s.terminalAndAnswer(events)
	s.Equal(harnessprotocol.TurnEventKindFailed, terminal, "无脚本假 CLI 退出应归 FAILED")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// workRoot 返回仓库根（go build 需在模块内执行）。
func workRoot() string {
	wd, _ := os.Getwd()
	// 本文件位于 tests/integration/agentteams/externalcli/codebuddy/，
	// 回退五级到仓库根
	return filepath.Clean(wd + "/../../../../..")
}

// newScriptedHarness 构造带脚本参数的 harness：
// 传输工厂在 harness 组装的基础参数后追加 --script，再走真子进程实现。
func (s *ProviderRoundtripSuite) newScriptedHarness(script string) *codebuddy.CodebuddyHarness {
	path := filepath.Join(s.scriptsDir, script)
	harness, err := codebuddy.NewCodebuddyHarnessWithFactory(
		codebuddy.CodebuddyConfig{CliPath: s.fakeBin, TurnTimeoutSeconds: 30},
		func(options codebuddy.TransportOptions) (codebuddy.Transport, error) {
			options.Args = append(options.Args, "--script", path)
			return codebuddy.NewProcessTransport(options)
		})
	s.Require().NoError(err)
	return harness
}

// newContext 构造协议上下文。
func (s *ProviderRoundtripSuite) newContext() harnessprotocol.HarnessContext {
	return harnessprotocol.HarnessContext{
		AgentName:       "e2e-codebuddy",
		AgentID:         "e2e_codebuddy",
		HostSessionID:   "e2e-host-session",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}
}

// writeScript 落盘帧脚本并返回路径。
func (s *ProviderRoundtripSuite) writeScript(name string, lines []string) string {
	path := filepath.Join(s.scriptsDir, name)
	s.Require().NoError(os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600))
	return path
}

// collectTurnEvents 消费单轮事件（终态后游标闭合）。
func (s *ProviderRoundtripSuite) collectTurnEvents(
	harness *codebuddy.CodebuddyHarness, turnID string) []harnessprotocol.HarnessEvent {
	id := turnID
	cursor := harness.TurnEvents(&id)
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

// assertTurnInvariants 单轮事件序不变量（对齐 _contract.py:104-113；
// 15.17 Task 10 提升为 testhelpers 共享助手后此处切换调用）。
func (s *ProviderRoundtripSuite) assertTurnInvariants(events []harnessprotocol.HarnessEvent, turnID string) {
	started := 0
	terminal := 0
	sequences := make([]int, 0, len(events))
	for _, event := range events {
		s.Equal(turnID, event.TurnID, "turn 视图串轮")
		sequences = append(sequences, event.Sequence)
		if payload, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok {
			if payload.Kind == harnessprotocol.TurnEventKindStarted {
				started++
			} else {
				terminal++
			}
		}
	}
	s.Equal(1, started, "恰一个 STARTED")
	s.Equal(1, terminal, "恰一个终态")
	for index := 1; index < len(sequences); index++ {
		s.Greater(sequences[index], sequences[index-1], "sequence 必须单调")
	}
}

// terminalAndAnswer 提取终态类型与 answer 文本。
func (s *ProviderRoundtripSuite) terminalAndAnswer(
	events []harnessprotocol.HarnessEvent) (harnessprotocol.TurnEventKind, string) {
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
