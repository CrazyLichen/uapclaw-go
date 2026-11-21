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
	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/codebuddy"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/dsh"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// dshFakeSession dsh fake 会话（15.17 双驱动路径之一；接口为 dsh 包导出面）。
type dshFakeSession struct {
	// sessionID StartSession 分配的会话标识（root 会话事件按此归属）
	sessionID string

	// notifications Run 期间回放的通知序列
	notifications []deepseekharness.Notification
	// result Run 返回值
	result *deepseekharness.RunResult
	// err Run 返回错误
	err error
}

// Run 回放通知并返回脚本终态。
func (f *dshFakeSession) Run(input any, onNotification func(deepseekharness.Notification)) (*deepseekharness.RunResult, error) {
	for _, notification := range f.notifications {
		onNotification(notification)
	}
	return f.result, f.err
}

// dshFakeHarness dsh fake 句柄。
type dshFakeHarness struct{ session *dshFakeSession }

// StartSession 返回 fake 会话并记录分配的会话标识。
func (f *dshFakeHarness) StartSession(sessionID string) (dsh.DshSDKSession, error) {
	f.session.sessionID = sessionID
	return f.session, nil
}

// Close 无操作。
func (f *dshFakeHarness) Close() error { return nil }

// EventOrderSuite 事件序不变量套件：dsh fake SDK + codebuddy 假 CLI 双驱动。
//
// 对齐 Python tests/system_tests/harness_providers/_contract.py 的
// run_text_turn + assert_turn_invariants 的非 LLM 份额（真 LLM 最小两条见
// tests/integration/real_llm）。跨子系统：harness_providers/dsh +
// harness_providers/codebuddy + harness_protocol + testhelpers。
type EventOrderSuite struct {
	suite.Suite
	// fakeBin codebuddy 假 CLI 可执行路径
	fakeBin string
	// scriptsDir 帧脚本目录
	scriptsDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEventOrderSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestEventOrderSuite(t *testing.T) {
	suite.Run(t, new(EventOrderSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// SetupSuite 构建 codebuddy 假 CLI。
func (s *EventOrderSuite) SetupSuite() {
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

// TestEventOrder_dshFakeSDK_text整轮 dsh fake SDK 驱动一轮：
// invariants + FINISHED + PONG（run_text_turn 的非 LLM 份额）。
func (s *EventOrderSuite) TestEventOrder_dshFakeSDK_text整轮() {
	session := &dshFakeSession{
		notifications: []deepseekharness.Notification{
			rootSessionEvent("assistant/chunk", map[string]any{
				"chunk": map[string]any{"type": "text-delta", "text": "PONG", "index": 0.0},
				"turn":  0.0, "step": 0.0,
			}),
			rootSessionEvent("turn/end", map[string]any{"reason": map[string]any{"type": "completed"}}),
		},
		result: &deepseekharness.RunResult{FinalResponse: "PONG", FinishReason: strPtrOf("completed")},
	}
	harness, err := dsh.NewDshHarnessWithFactory(dsh.DshHarnessConfig{DshHome: "/tmp/dsh-home"},
		func(options dsh.GoSDKOptions) (dsh.DshSDKHarness, error) {
			return &dshFakeHarness{session: session}, nil
		})
	s.Require().NoError(err)
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), newProtocolContext("dsh-e2e")))

	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "Reply PONG"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectEvents(harness.TurnEvents(&receipt.TurnID))
	testhelpers.AssertTurnInvariants(s.T(), events, receipt.TurnID)
	kind, answer := terminalAndAnswerOf(events)
	s.Equal(harnessprotocol.TurnEventKindFinished, kind)
	s.Contains(answer, "PONG")
}

// TestEventOrder_codebuddy假CLI_text整轮 codebuddy 假 CLI 真子进程驱动一轮。
func (s *EventOrderSuite) TestEventOrder_codebuddy假CLI_text整轮() {
	script := filepath.Join(s.scriptsDir, "order_success.jsonl")
	s.Require().NoError(os.WriteFile(script, []byte(strings.Join([]string{
		`{"type":"assistant","session_id":"fake-session-1","message":{"content":[{"type":"text","text":"PONG"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"PONG","duration_ms":100}`,
	}, "\n")), 0o600))
	harness := s.newCodebuddyHarness(script)
	defer func() { _ = harness.Stop(context.Background()) }()
	s.Require().NoError(harness.Start(context.Background(), newProtocolContext("codebuddy-e2e")))

	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "Reply PONG"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectEvents(harness.TurnEvents(&receipt.TurnID))
	testhelpers.AssertTurnInvariants(s.T(), events, receipt.TurnID)
	kind, answer := terminalAndAnswerOf(events)
	s.Equal(harnessprotocol.TurnEventKindFinished, kind)
	s.Contains(answer, "PONG")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// workRoot 返回仓库根。
func workRoot() string {
	wd, _ := os.Getwd()
	// 本文件位于 tests/integration/agentcore/harness_protocol/，回退五级到仓库根
	return filepath.Clean(wd + "/../../../..")
}

// newProtocolContext 构造协议上下文。
func newProtocolContext(agentName string) harnessprotocol.HarnessContext {
	return harnessprotocol.HarnessContext{
		AgentName:       agentName,
		AgentID:         "e2e_" + agentName,
		HostSessionID:   "e2e-host-session",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}
}

// rootSessionEvent 构造 dsh root 会话事件通知。
func rootSessionEvent(eventType string, data map[string]any) deepseekharness.Notification {
	return deepseekharness.Notification{Method: "session.event", Payload: map[string]any{
		"event": map[string]any{"type": eventType, "data": data},
	}}
}

// strPtrOf 字符串指针。
func strPtrOf(value string) *string { return &value }

// newCodebuddyHarness 构造带脚本参数的 codebuddy harness（真子进程）。
func (s *EventOrderSuite) newCodebuddyHarness(script string) *codebuddy.CodebuddyHarness {
	harness, err := codebuddy.NewCodebuddyHarnessWithFactory(
		codebuddy.CodebuddyConfig{CliPath: s.fakeBin, TurnTimeoutSeconds: 30},
		func(options codebuddy.TransportOptions) (codebuddy.Transport, error) {
			options.Args = append(options.Args, "--script", script)
			return codebuddy.NewProcessTransport(options)
		})
	s.Require().NoError(err)
	return harness
}

// collectEvents 消费游标全部事件。
func (s *EventOrderSuite) collectEvents(cursor harnessprotocol.HarnessEventCursor) []harnessprotocol.HarnessEvent {
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

// terminalAndAnswerOf 提取终态类型与 answer 文本。
func terminalAndAnswerOf(events []harnessprotocol.HarnessEvent) (harnessprotocol.TurnEventKind, string) {
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
