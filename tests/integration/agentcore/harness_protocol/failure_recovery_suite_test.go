//go:build integration

package harness_protocol

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

// FailureRecoverySuite 失败恢复套件：崩溃 / 鉴权失败 / 超时 × transport 层
// （15.17 方案 B：打在 transport/externalcli 层；provider 层份额 ⤵️15.7/15.8，
// 见 spec 2026-12-06 §2 裁决 1）。
type FailureRecoverySuite struct {
	suite.Suite
	// fakeBin codebuddy 假 CLI 可执行路径
	fakeBin string
	// scriptsDir 帧脚本目录
	scriptsDir string
	// extraFlags 本用例追加的假 CLI 故障开关
	extraFlags []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestFailureRecoverySuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestFailureRecoverySuite(t *testing.T) {
	suite.Run(t, new(FailureRecoverySuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// SetupSuite 构建 codebuddy 假 CLI。
func (s *FailureRecoverySuite) SetupSuite() {
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

// TestFailureRecovery_崩溃 假 CLI 中途 exit 1 → FAILED + 结构化错误。
func (s *FailureRecoverySuite) TestFailureRecovery_崩溃() {
	script := s.writeScript("crash.jsonl", []string{
		`{"type":"assistant","session_id":"fake-session-1","message":{"content":[{"type":"text","text":"PART"}]}}`,
	}, "--crash-after", "1")
	harness := s.newHarness(script)
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), newProtocolContext("crash-e2e")))
	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectEvents(harness.TurnEvents(&receipt.TurnID))
	testhelpers.AssertTurnInvariants(s.T(), events, receipt.TurnID)
	kind, _ := terminalAndAnswerOf(events)
	s.Equal(harnessprotocol.TurnEventKindFailed, kind, "进程崩溃应归 FAILED")
}

// TestFailureRecovery_鉴权失败 假 CLI result error + exit 1 → FAILED，
// 错误文案携带鉴权语义。
func (s *FailureRecoverySuite) TestFailureRecovery_鉴权失败() {
	harness := s.newHarness("", "--auth-fail")
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), newProtocolContext("auth-e2e")))
	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectEvents(harness.TurnEvents(&receipt.TurnID))
	kind, _ := terminalAndAnswerOf(events)
	s.Equal(harnessprotocol.TurnEventKindFailed, kind)
	// 错误文案含鉴权语义（result 帧文本）
	found := false
	for _, event := range events {
		if payload, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok &&
			payload.Kind == harnessprotocol.TurnEventKindFailed && payload.Result != nil &&
			payload.Result.Error != nil &&
			strings.Contains(payload.Result.Error.Message, "authentication failed") {
			found = true
		}
	}
	s.True(found, "错误文案应携带鉴权语义")
}

// TestFailureRecovery_挂起超时 假 CLI 挂起 → 单轮超时归 FAILED。
func (s *FailureRecoverySuite) TestFailureRecovery_挂起超时() {
	harness := s.newHarness("", "--hang")
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), newProtocolContext("hang-e2e")))
	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectEvents(harness.TurnEvents(&receipt.TurnID))
	testhelpers.AssertTurnInvariants(s.T(), events, receipt.TurnID)
	kind, _ := terminalAndAnswerOf(events)
	s.Equal(harnessprotocol.TurnEventKindFailed, kind, "挂起应按超时归 FAILED")
}

// TestFailureRecovery_dsh侧 dsh fake SDK Run 错误 → FAILED（对照路径）。
func (s *FailureRecoverySuite) TestFailureRecovery_dsh侧() {
	session := &dshFakeSession{err: context.DeadlineExceeded}
	harness, err := dsh.NewDshHarnessWithFactory(dsh.DshHarnessConfig{DshHome: "/tmp/dsh-home"},
		func(options dsh.GoSDKOptions) (dsh.DshSDKHarness, error) {
			return &dshFakeHarness{session: session}, nil
		})
	s.Require().NoError(err)
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), newProtocolContext("dsh-fail-e2e")))
	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectEvents(harness.TurnEvents(&receipt.TurnID))
	testhelpers.AssertTurnInvariants(s.T(), events, receipt.TurnID)
	kind, _ := terminalAndAnswerOf(events)
	s.Equal(harnessprotocol.TurnEventKindFailed, kind)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeScript 落盘脚本并记录本用例附加开关。
func (s *FailureRecoverySuite) writeScript(name string, lines []string, extra ...string) string {
	path := filepath.Join(s.scriptsDir, name)
	s.Require().NoError(os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600))
	s.extraFlags = extra
	return path
}

// newHarness 构造带脚本（可选）与故障开关（可选）的 codebuddy harness。
func (s *FailureRecoverySuite) newHarness(script string, flags ...string) *codebuddy.CodebuddyHarness {
	var scriptPath string
	if script != "" {
		scriptPath = filepath.Join(s.scriptsDir, script)
	}
	harness, err := codebuddy.NewCodebuddyHarnessWithFactory(
		codebuddy.CodebuddyConfig{CliPath: s.fakeBin, TurnTimeoutSeconds: 3},
		func(options codebuddy.TransportOptions) (codebuddy.Transport, error) {
			if scriptPath != "" {
				options.Args = append(options.Args, "--script", scriptPath)
			}
			options.Args = append(options.Args, s.extraFlags...)
			options.Args = append(options.Args, flags...)
			return codebuddy.NewProcessTransport(options)
		})
	s.Require().NoError(err)
	return harness
}

// collectEvents 消费游标全部事件。
func (s *FailureRecoverySuite) collectEvents(cursor harnessprotocol.HarnessEventCursor) []harnessprotocol.HarnessEvent {
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
